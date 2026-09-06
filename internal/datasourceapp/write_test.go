package datasourceapp

import (
	"strings"
	"testing"
)

func TestLocalWriteSQLSingleReviewedOperation(t *testing.T) {
	for _, sql := range []string{"INSERT INTO stock(id) VALUES(1)", "UPDATE stock SET note='x;y' WHERE id=1;", "DELETE FROM stock WHERE id=1", "CREATE TABLE x(id int)", "CREATE UNIQUE INDEX ix ON x(id)", "ALTER TABLE x ADD COLUMN a int", "DROP TABLE x"} {
		if err := ValidateLocalWriteSQL(sql); err != nil {
			t.Errorf("valid %q: %v", sql, err)
		}
	}
	for _, sql := range []string{"", "DELETE FROM x; DELETE FROM y", "UPDATE x SET n=1 /*comment*/", "GRANT ALL ON x TO other", "CREATE DATABASE private", "CREATE FUNCTION f() RETURNS int AS $$ SELECT 1 $$ LANGUAGE SQL", "SELECT 1", "INSERT INTO x SELECT LOAD_FILE('/secret')", "INSERT INTO x VALUES(pg_read_file('/secret'))", "UPDATE x SET a='unterminated", strings.Repeat("x", 16385)} {
		if err := ValidateLocalWriteSQL(sql); err == nil {
			t.Errorf("unsafe %q accepted", sql)
		}
	}
}
