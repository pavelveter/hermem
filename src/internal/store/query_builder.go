package store

import (
	"fmt"
	"strings"
)

// SQLBuilder is a minimal query builder for SQL queries.
// Not an ORM — just helps compose WHERE clauses, ORDER BY, and LIMIT
// without string concatenation.
type SQLBuilder struct {
	base  string
	wheres []string
	args  []interface{}
	order string
	limit int
}

// NewSQLBuilder creates a new SQLBuilder with a base SELECT query.
func NewSQLBuilder(base string) *SQLBuilder {
	return &SQLBuilder{base: base}
}

// Where adds a condition to the WHERE clause.
func (b *SQLBuilder) Where(cond string, args ...interface{}) *SQLBuilder {
	b.wheres = append(b.wheres, cond)
	b.args = append(b.args, args...)
	return b
}

// WhereIf conditionally adds a WHERE clause.
func (b *SQLBuilder) WhereIf(cond string, args ...interface{}) *SQLBuilder {
	if len(args) > 0 {
		for _, arg := range args {
			if arg == nil || arg == "" || arg == 0 {
				return b
			}
		}
	}
	b.wheres = append(b.wheres, cond)
	b.args = append(b.args, args...)
	return b
}

// Order sets the ORDER BY clause.
func (b *SQLBuilder) Order(order string) *SQLBuilder {
	b.order = order
	return b
}

// Limit sets the LIMIT clause.
func (b *SQLBuilder) Limit(n int) *SQLBuilder {
	b.limit = n
	return b
}

// Args returns the accumulated arguments.
func (b *SQLBuilder) Args() []interface{} {
	return b.args
}

// Build constructs the final SQL query string.
func (b *SQLBuilder) Build() string {
	var sb strings.Builder
	sb.WriteString(b.base)

	if len(b.wheres) > 0 {
		sb.WriteString(" WHERE ")
		sb.WriteString(strings.Join(b.wheres, " AND "))
	}

	if b.order != "" {
		sb.WriteString(" ORDER BY ")
		sb.WriteString(b.order)
	}

	if b.limit > 0 {
		sb.WriteString(fmt.Sprintf(" LIMIT %d", b.limit))
	}

	return sb.String()
}
