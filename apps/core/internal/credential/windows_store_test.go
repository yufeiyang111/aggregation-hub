//go:build windows

package credential

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"testing"
)

func TestWindowsStoreIntegrationContract(t *testing.T) {
	store := NewWindowsStore()
	suffix := make([]byte, 12)
	if _, err := rand.Read(suffix); err != nil {
		t.Fatal(err)
	}
	ref := Ref("integration/" + hex.EncodeToString(suffix))
	t.Cleanup(func() { _ = store.Delete(context.Background(), ref) })
	runStoreContractWithRef(t, store, ref)
}
