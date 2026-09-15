package sqlite

import (
	"errors"
	"fmt"
	"strings"
)

var ErrUnknownSchema = errors.New("unknown upgrade schema")

var reservedModelOfficeMigrations = []string{
	"0157_model_native_v2.sql",
	"0158_execution_contract_v2.sql",
	"0159_office_delivery_v2.sql",
}

func RefuseUnknownUpgradeSchema(applied []string) error {
	known := make(map[string]bool, len(manifest))
	for _, m := range manifest {
		known[m.name] = true
	}
	for _, name := range applied {
		if !known[name] {
			return fmt.Errorf("%w: %s", ErrUnknownSchema, name)
		}
	}
	return nil
}

func reservedModelOfficeCollision(applied []string) error {
	for _, name := range applied {
		if strings.HasPrefix(name, "0155_model_native") || strings.HasPrefix(name, "0156_execution_contract") || strings.HasPrefix(name, "0157_office_delivery") {
			return fmt.Errorf("%w: stale pack number still present: %s", ErrUnknownSchema, name)
		}
	}
	return nil
}
