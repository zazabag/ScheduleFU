package host

import "testing"

func TestPamyatIzProcMeminfo(t *testing.T) {
	total, avail := parseMeminfo("MemTotal:        8131584 kB\nMemFree:          715000 kB\nMemAvailable:    7436288 kB\n")
	if total != 7941 || avail != 7262 {
		t.Errorf("память: %d / %d", total, avail)
	}
	if v := firstFloat("0.07 0.02 0.00 1/180 12345\n"); v != 0.07 {
		t.Errorf("нагрузка: %v", v)
	}
}
