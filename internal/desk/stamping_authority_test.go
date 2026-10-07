package desk

// A time-stamping authority for tests, trimmed from the runtime's own
// (runtime 0.27.1, `internal/timestamp/tsatest`): a root, a time-stamping
// certificate under it, and RFC 3161 replies made with them, encoded with
// encoding/asn1 rather than with anything the runtime's verifier shares, so
// the two meet only at the bytes. Only the shape the runtime's own tests
// call well formed is kept: none of its faults.

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net/http"
	"sort"
	"sync"
	"time"
)

var (
	tsaOIDSignedData           = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 2}
	tsaOIDTSTInfo              = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 1, 4}
	tsaOIDContentType          = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 3}
	tsaOIDMessageDigest        = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 4}
	tsaOIDSigningCertificateV2 = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 2, 47}
	tsaOIDSHA256               = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}
	tsaOIDECDSAWithSHA256      = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 2}
	tsaOIDTimeStamping         = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 3, 8}
	// testTSAPolicy is the policy the test authority stamps under.
	testTSAPolicy = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 99999, 1}
)

// testAuthority is a root and a time-stamping certificate under it, serving
// RFC 3161 over HTTP.
type testAuthority struct {
	root      *x509.Certificate
	rootKey   *ecdsa.PrivateKey
	signer    *x509.Certificate
	signerKey *ecdsa.PrivateKey

	mu       sync.Mutex
	serial   int64
	requests int
}

// newTestAuthority makes an authority whose root is valid ten years either
// side of now, and whose time-stamping certificate one year either side.
func newTestAuthority() (*testAuthority, error) {
	rootKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	rootTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test time-stamping root"},
		NotBefore: now.Add(-10 * 365 * 24 * time.Hour), NotAfter: now.Add(10 * 365 * 24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	rootDER, err := x509.CreateCertificate(rand.Reader, rootTemplate, rootTemplate, &rootKey.PublicKey, rootKey)
	if err != nil {
		return nil, err
	}
	root, err := x509.ParseCertificate(rootDER)
	if err != nil {
		return nil, err
	}
	signerKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	usage, err := asn1.Marshal([]asn1.ObjectIdentifier{tsaOIDTimeStamping})
	if err != nil {
		return nil, err
	}
	signerTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "test time-stamping"},
		NotBefore: now.Add(-365 * 24 * time.Hour), NotAfter: now.Add(365 * 24 * time.Hour),
		KeyUsage:        x509.KeyUsageDigitalSignature,
		ExtraExtensions: []pkix.Extension{{Id: asn1.ObjectIdentifier{2, 5, 29, 37}, Critical: true, Value: usage}},
	}
	signerDER, err := x509.CreateCertificate(rand.Reader, signerTemplate, root, &signerKey.PublicKey, rootKey)
	if err != nil {
		return nil, err
	}
	signer, err := x509.ParseCertificate(signerDER)
	if err != nil {
		return nil, err
	}
	return &testAuthority{root: root, rootKey: rootKey, signer: signer, signerKey: signerKey}, nil
}

// rootPEM is the root, PEM, as a verifier is given it.
func (a *testAuthority) rootPEM() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: a.root.Raw})
}

// crl is a revocation list issued by the root at thisUpdate that revokes
// nothing, PEM.
func (a *testAuthority) crl(thisUpdate time.Time) ([]byte, error) {
	der, err := x509.CreateRevocationList(rand.Reader, &x509.RevocationList{Number: big.NewInt(1), ThisUpdate: thisUpdate, NextUpdate: thisUpdate.Add(24 * time.Hour)}, a.root, a.rootKey)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "X509 CRL", Bytes: der}), nil
}

// served is how many requests the authority has answered.
func (a *testAuthority) served() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.requests
}

type tsaAlgorithm struct {
	Algorithm  asn1.ObjectIdentifier
	Parameters asn1.RawValue `asn1:"optional"`
}

type tsaImprint struct {
	HashAlgorithm tsaAlgorithm
	HashedMessage []byte
}

type tsaInfo struct {
	Version        int
	Policy         asn1.ObjectIdentifier
	MessageImprint tsaImprint
	SerialNumber   *big.Int
	GenTime        asn1.RawValue
	Nonce          *big.Int `asn1:"optional"`
}

type tsaAttribute struct {
	Type   asn1.ObjectIdentifier
	Values []asn1.RawValue `asn1:"set"`
}

type tsaCertID struct {
	CertHash []byte
}

type tsaSigningCertificate struct {
	Certs []tsaCertID
}

type tsaIssuerSerial struct {
	Issuer asn1.RawValue
	Serial *big.Int
}

type tsaSignerInfo struct {
	Version            int
	SID                tsaIssuerSerial
	DigestAlgorithm    tsaAlgorithm
	SignedAttrs        asn1.RawValue
	SignatureAlgorithm tsaAlgorithm
	Signature          []byte
}

type tsaEncapsulated struct {
	EContentType asn1.ObjectIdentifier
	EContent     asn1.RawValue
}

type tsaSignedData struct {
	Version          int
	DigestAlgorithms []tsaAlgorithm `asn1:"set"`
	EncapContentInfo tsaEncapsulated
	Certificates     asn1.RawValue
	SignerInfos      []tsaSignerInfo `asn1:"set"`
}

type tsaContentInfo struct {
	ContentType asn1.ObjectIdentifier
	Content     asn1.RawValue
}

type tsaRequest struct {
	Version        int
	MessageImprint tsaImprint
	ReqPolicy      asn1.ObjectIdentifier `asn1:"optional"`
	Nonce          *big.Int              `asn1:"optional"`
	CertReq        bool                  `asn1:"optional,default:false"`
}

type tsaStatus struct {
	Status int
}

type tsaReply struct {
	Status tsaStatus
	Token  asn1.RawValue `asn1:"optional"`
}

// token is a time-stamp token over a SHA-256 digest at now, in whole
// seconds, with nonce, DER.
func (a *testAuthority) token(digest []byte, nonce *big.Int) ([]byte, error) {
	a.mu.Lock()
	a.serial++
	serial := a.serial
	a.mu.Unlock()
	content, err := asn1.Marshal(tsaInfo{
		Version: 1, Policy: testTSAPolicy,
		MessageImprint: tsaImprint{HashAlgorithm: tsaAlgorithm{Algorithm: tsaOIDSHA256, Parameters: asn1.NullRawValue}, HashedMessage: digest},
		SerialNumber:   big.NewInt(serial),
		GenTime:        asn1.RawValue{Class: asn1.ClassUniversal, Tag: asn1.TagGeneralizedTime, Bytes: []byte(time.Now().UTC().Truncate(time.Second).Format("20060102150405Z"))},
		Nonce:          nonce,
	})
	if err != nil {
		return nil, err
	}
	raw := func(value any) (asn1.RawValue, error) {
		der, err := asn1.Marshal(value)
		return asn1.RawValue{FullBytes: der}, err
	}
	contentSum := sha256.Sum256(content)
	certSum := sha256.Sum256(a.signer.Raw)
	contentType, err := raw(tsaOIDTSTInfo)
	if err != nil {
		return nil, err
	}
	messageDigest, err := raw(contentSum[:])
	if err != nil {
		return nil, err
	}
	binding, err := raw(tsaSigningCertificate{Certs: []tsaCertID{{CertHash: certSum[:]}}})
	if err != nil {
		return nil, err
	}
	attributes, err := asn1.MarshalWithParams([]tsaAttribute{
		{Type: tsaOIDContentType, Values: []asn1.RawValue{contentType}},
		{Type: tsaOIDMessageDigest, Values: []asn1.RawValue{messageDigest}},
		{Type: tsaOIDSigningCertificateV2, Values: []asn1.RawValue{binding}},
	}, "set")
	if err != nil {
		return nil, err
	}
	attributesSum := sha256.Sum256(attributes)
	signature, err := a.signerKey.Sign(rand.Reader, attributesSum[:], crypto.SHA256)
	if err != nil {
		return nil, err
	}
	octets, err := asn1.Marshal(content)
	if err != nil {
		return nil, err
	}
	// The certificate set in DER order, as a SET OF is.
	pair := [][]byte{a.root.Raw, a.signer.Raw}
	sort.Slice(pair, func(i, j int) bool { return bytes.Compare(pair[i], pair[j]) < 0 })
	signed, err := asn1.Marshal(tsaSignedData{
		Version:          3,
		DigestAlgorithms: []tsaAlgorithm{{Algorithm: tsaOIDSHA256}},
		EncapContentInfo: tsaEncapsulated{EContentType: tsaOIDTSTInfo, EContent: asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: octets}},
		Certificates:     asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: bytes.Join(pair, nil)},
		SignerInfos: []tsaSignerInfo{{
			Version:            1,
			SID:                tsaIssuerSerial{Issuer: asn1.RawValue{FullBytes: a.signer.RawIssuer}, Serial: a.signer.SerialNumber},
			DigestAlgorithm:    tsaAlgorithm{Algorithm: tsaOIDSHA256},
			SignedAttrs:        asn1.RawValue{FullBytes: append([]byte{0xa0}, attributes[1:]...)},
			SignatureAlgorithm: tsaAlgorithm{Algorithm: tsaOIDECDSAWithSHA256},
			Signature:          signature,
		}},
	})
	if err != nil {
		return nil, err
	}
	return asn1.Marshal(tsaContentInfo{ContentType: tsaOIDSignedData, Content: asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: signed}})
}

// reply answers a request as an authority does: a granted reply carrying the
// token over the request's digest, with its nonce.
func (a *testAuthority) reply(request []byte) ([]byte, error) {
	var parsed tsaRequest
	if rest, err := asn1.Unmarshal(request, &parsed); err != nil || len(rest) != 0 {
		return nil, errors.New("not a time-stamp request")
	}
	token, err := a.token(parsed.MessageImprint.HashedMessage, parsed.Nonce)
	if err != nil {
		return nil, err
	}
	return asn1.Marshal(tsaReply{Status: tsaStatus{Status: 0}, Token: asn1.RawValue{FullBytes: token}})
}

// ServeHTTP answers RFC 3161 requests over HTTP.
func (a *testAuthority) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	a.requests++
	a.mu.Unlock()
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if err != nil || r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/timestamp-query" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	answer, err := a.reply(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/timestamp-reply")
	_, _ = w.Write(answer)
}
