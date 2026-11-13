//go:build !dot && !panel

package assistant

import "github.com/HuskerMinion/techo5/echod/internal/hardware/lenscover"

func lensCovered() bool { return lenscover.Get().Covered() }

func isCameraOff(error) bool { return false }
