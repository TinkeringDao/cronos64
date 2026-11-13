//go:build panel

package assistant

import (
	"errors"

	"github.com/HuskerMinion/techo5/echod/internal/hardware/camera"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/lenscover"
)

func lensCovered() bool { return lenscover.Get().Covered() }

func isCameraOff(err error) bool { return errors.Is(err, camera.ErrOff) }
