//go:build !linux && !darwin && !windows

package probe

import (
	"context"
	"errors"
)

func diskFree(string) (uint64, error) { return 0, errors.New("not supported on this OS") }

func memTotal(context.Context) (uint64, error) { return 0, errors.New("not supported on this OS") }
