package app

import (
	"github.com/jitsucom/bulker/base/logging"
	"github.com/jitsucom/bulker/base/uuid"
	"testing"
)

// test hashToken function of router
func TestHashToken(t *testing.T) {
	token := "21a2ae36-32994870a9fbf2f61ea6f6c8"
	salt := uuid.New()
	secret := "dea42a58-acf4-45af-85bb-e77e94bd5025"
	hashedToken := hashToken(token, salt, secret)
	logging.Infof("hashedToken: %s.%s", salt, hashedToken)
}
