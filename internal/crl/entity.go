package crl

import (
	"crypto/sha1"
	"fmt"
)

type RevokedCertificate struct {
	Reason      string
	Certificate []byte
	Thumbprint  [sha1.Size]byte
}

func (rc *RevokedCertificate) Hash() string {
	return fmt.Sprintf("%x", rc.Thumbprint)
}

func (rc *RevokedCertificate) String() string {
	return fmt.Sprintf("Reason=%s", rc.Reason)
}
