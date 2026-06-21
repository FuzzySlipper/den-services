package visualcontract

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
)

func TestValidateGoldenFixtures(t *testing.T) {
	for _, path := range []string{
		"../testdata/contracts/reference.web-ui.json",
		"../testdata/contracts/candidate.pass.web-ui.json",
		"../testdata/contracts/candidate.fail.web-ui.json",
		"../testdata/asha/asha-studio.contract.json",
	} {
		path := path
		t.Run(path, func(t *testing.T) {
			contract := loadContractFixture(t, path)
			if err := ValidateContract(&contract); err != nil {
				t.Fatalf("ValidateContract() error = %v", err)
			}
		})
	}
}

func TestValidateContractRejectsInvalidShapes(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Contract)
	}{
		{
			name: "missing object id",
			mutate: func(contract *Contract) {
				contract.Objects[0].ID = ""
			},
		},
		{
			name: "bad relation ref",
			mutate: func(contract *Contract) {
				contract.Relations = append(contract.Relations, Relation{
					Type:       RelationAbove,
					A:          "missing",
					B:          "primary_cta",
					Confidence: 0.9,
				})
			},
		},
		{
			name: "out of range bounds",
			mutate: func(contract *Contract) {
				contract.Objects[0].Bounds.X = 0.9
				contract.Objects[0].Bounds.W = 0.2
			},
		},
		{
			name: "invalid importance",
			mutate: func(contract *Contract) {
				contract.Constraints[0].Importance = "urgent"
			},
		},
		{
			name: "bad evidence ref",
			mutate: func(contract *Contract) {
				contract.Objects[0].EvidenceRefs = []string{"missing"}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			contract := loadContractFixture(t, "../testdata/contracts/reference.web-ui.json")
			tt.mutate(&contract)
			err := ValidateContract(&contract)
			if !errors.Is(err, ErrInvalidContract) {
				t.Fatalf("ValidateContract() error = %v, want ErrInvalidContract", err)
			}
		})
	}
}

func loadContractFixture(t *testing.T, path string) Contract {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	var contract Contract
	if err := json.Unmarshal(data, &contract); err != nil {
		t.Fatalf("Unmarshal(%s) error = %v", path, err)
	}
	return contract
}
