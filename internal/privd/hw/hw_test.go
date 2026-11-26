package hw

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadRAPLTopLevelOnly(t *testing.T) {
	root := t.TempDir()
	for i, name := range []string{"package-0", "package-1"} {
		d := "sys/class/powercap/intel-rapl:" + string(rune('0'+i))
		write(t, root, d+"/name", name+"\n")
		write(t, root, d+"/energy_uj", "123456\n")
		write(t, root, d+"/max_energy_range_uj", "262143328850\n")
	}
	// a core sub-zone must be ignored
	write(t, root, "sys/class/powercap/intel-rapl:0:0/name", "core\n")
	write(t, root, "sys/class/powercap/intel-rapl:0:0/energy_uj", "1\n")
	z := ReadRAPL(root)
	if len(z) != 2 || z[0].Name != "package-0" || z[1].Name != "package-1" || z[0].EnergyUJ != 123456 || z[0].MaxUJ != 262143328850 {
		t.Fatalf("zones: %+v", z)
	}
	if got := ReadRAPL(t.TempDir()); len(got) != 0 {
		t.Fatalf("no powercap dir must give no zones: %+v", got)
	}
}
