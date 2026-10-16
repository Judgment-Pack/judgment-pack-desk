package desk

import (
	"context"
	"os"
	"testing"
)

func TestExchangePackRangesUseRuntime(t *testing.T) {
	bin := os.Getenv("JPACK_EXCHANGE_TEST_BIN")
	if bin == "" {
		t.Skip("requires Runtime")
	}
	s := &Server{cfg: Config{JpackBin: bin}}
	for _, tt := range []struct {
		version, expression string
		accepted            bool
	}{
		{"1.2.3", ">=1.0.0 <2.0.0", true},
		{"1.2.3", "1.2.3", true},
		{"2.0.0", ">=1.0.0 <2.0.0", false},
		{"1.2.3", "^1.0.0", false},
		{"1.2.3", "", false},
		{"1.2.3", ">=01.0.0", false},
	} {
		err := s.checkExchangePackRange(context.Background(), tt.version, tt.expression)
		if (err == nil) != tt.accepted {
			t.Fatalf("%+v: %v", tt, err)
		}
	}
}
func TestExchangePreviewLeavesGenericRequestsUntouched(t *testing.T) {
	s := &Server{}
	raw := []byte("{ \"pack\": \"original bytes\", \"input\": {\"facts\":{\"n\":1.00}} }")
	got, err := s.checkExchangePreview(context.Background(), raw)
	if err != nil || string(got) != string(raw) {
		t.Fatal(string(got), err)
	}
	for _, raw := range []string{"{\"exchangeImportId\":null}", "{\"exchangeImportId\":\"bad\"}", "{\"exchangeImportId\":\"bad\",\"exchangeImportId\":\"also-bad\"}"} {
		if _, err = s.checkExchangePreview(context.Background(), []byte(raw)); err == nil {
			t.Fatal("invalid origin accepted")
		}
	}
}
