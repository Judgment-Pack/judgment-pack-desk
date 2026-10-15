package desk

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
)

const retainedArtifactDirectory = ".desk-private/retained-artifacts"

type fileRetainedArtifactRepository struct{ open func(bool) (*os.Root, error) }

func (s *Server) openRetainedArtifacts(create bool) (*os.Root, error) {
	root, made, err := openOwnFolder(s.root, strings.Split(retainedArtifactDirectory, "/"), create, "retained artifacts")
	if err == nil && made {
		err = writePrivateData(root, ".gitignore", []byte("*\n"))
		if err != nil {
			root.Close()
		}
	}
	return root, err
}
func (f fileRetainedArtifactRepository) Get(ctx context.Context, ref retainedArtifactRef) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !ref.valid() {
		return nil, errRetainedArtifactChanged
	}
	root, err := f.open(false)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	raw, err := readPrivateData(root, ref.SHA256, ref.Bytes)
	if err == nil {
		err = verifyRetainedArtifact(ref, raw)
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return nil, err
	}
	return raw, nil
}
func (f fileRetainedArtifactRepository) Put(ctx context.Context, raw []byte) (retainedArtifactRef, error) {
	if err := ctx.Err(); err != nil {
		return retainedArtifactRef{}, err
	}
	if len(raw) > retainedArtifactByteLimit {
		return retainedArtifactRef{}, errors.New("retained artifact exceeds its storage limit")
	}
	ref := artifactReference(raw)
	root, err := f.open(true)
	if err != nil {
		return retainedArtifactRef{}, err
	}
	defer root.Close()
	lock, err := root.Open(".")
	if err != nil {
		return retainedArtifactRef{}, err
	}
	defer lock.Close()
	if err = waitPrivateDataLock(ctx, lock, true); err != nil {
		return retainedArtifactRef{}, err
	}
	prior, err := readPrivateData(root, ref.SHA256, retainedArtifactByteLimit)
	if err == nil {
		if !bytes.Equal(raw, prior) {
			return retainedArtifactRef{}, errRetainedArtifactChanged
		}
		return ref, nil // exact repeated content needs no write
	}
	if !errors.Is(err, os.ErrNotExist) {
		return retainedArtifactRef{}, err
	}
	if err = ctx.Err(); err != nil {
		return retainedArtifactRef{}, err
	}
	// Directory exclusion covers the absence check and staged publication. The
	// shared primitive syncs and reads back the file before acknowledging it.
	if err = writePrivateData(root, ref.SHA256, raw); err != nil {
		return retainedArtifactRef{}, err
	}
	return ref, nil
}
