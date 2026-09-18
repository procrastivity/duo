//go:build !linux

package portablelauncher

import "fmt"

func publishBundle(_, _ string) error {
	return fmt.Errorf("portable launcher conformance bundle publication requires Linux renameat2")
}
