//go:build !linux

package host

import "errors"

// disk на машине разработчика не считается: присмотр живёт на Linux.
func disk(string) (float64, float64, error) {
	return 0, 0, errors.New("диск считается только на Linux")
}
