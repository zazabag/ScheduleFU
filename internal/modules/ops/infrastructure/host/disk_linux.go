//go:build linux

package host

import "syscall"

// disk — всего и свободно (для непривилегированного) в гигабайтах.
func disk(path string) (total, free float64, err error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	const gib = 1 << 30
	return float64(st.Blocks) * float64(st.Bsize) / gib, float64(st.Bavail) * float64(st.Bsize) / gib, nil
}
