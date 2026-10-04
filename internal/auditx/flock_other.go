//go:build !unix

package auditx

import (
	"errors"
	"os"
)

// Native Windows is out of scope: limited actions fail closed.
func lockFile(*os.File) error { return errors.New("cross-process rate limits need a unix platform") }

func unlockFile(*os.File) {}
