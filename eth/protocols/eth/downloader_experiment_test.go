package eth

import (
	"math/big"
	"testing"
)

func TestLoadScrollExperiment(t *testing.T) {
	getenv := func(values map[string]string) func(string) string {
		return func(key string) string { return values[key] }
	}
	base := map[string]string{
		scrollExperimentEnabledEnv:  "true",
		scrollExperimentNetworkEnv:  "1714",
		scrollExperimentVictimsEnv:  "ab12cd",
		scrollExperimentTDOffsetEnv: "10000000",
	}
	cfg, err := loadScrollExperiment(getenv(base))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.networkID != 1714 || cfg.tdOffset.Cmp(big.NewInt(10000000)) != 0 {
		t.Fatalf("unexpected experiment config: %+v", cfg)
	}
	if _, ok := cfg.victims["ab12cd"]; !ok {
		t.Fatal("victim ID was not normalized")
	}

	delete(base, scrollExperimentVictimsEnv)
	if _, err := loadScrollExperiment(getenv(base)); err == nil {
		t.Fatal("accepted enabled config without a victim ID")
	}
}

func TestNormalizeScrollExperimentID(t *testing.T) {
	got := normalizeScrollExperimentID(" ENODE://AB12CD@127.0.0.1:30303?discport=30303 ")
	if got != "ab12cd" {
		t.Fatalf("normalized ID = %q", got)
	}
}
