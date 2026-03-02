package stdlib

import (
	"crypto/rand"
	"fmt"

	"github.com/lemonberrylabs/gcw-emulator/pkg/types"
)

// registerUUID registers uuid.* functions.
func (r *Registry) registerUUID() {
	r.Register("uuid.generate", uuidGenerate)
}

func uuidGenerate(args []types.Value) (types.Value, error) {
	uuid, err := generateUUIDv4()
	if err != nil {
		return types.Null, fmt.Errorf("uuid.generate: %v", err)
	}
	return types.NewString(uuid), nil
}

// generateUUIDv4 generates a random UUID v4.
func generateUUIDv4() (string, error) {
	var uuid [16]byte
	_, err := rand.Read(uuid[:])
	if err != nil {
		return "", err
	}

	// Set version (4) and variant (RFC 4122)
	uuid[6] = (uuid[6] & 0x0f) | 0x40
	uuid[8] = (uuid[8] & 0x3f) | 0x80

	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:16]), nil
}
