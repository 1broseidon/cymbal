package parser

import (
	"testing"

	"github.com/1broseidon/cymbal/lang"
)

func TestFeatureSQLConformanceVectors(t *testing.T) {
	vectors := []struct {
		name   string
		src    string
		checks []struct{ name, kind string }
	}{
		{
			name: "create table orders",
			src: `CREATE TABLE orders (
    id SERIAL PRIMARY KEY,
    amount NUMERIC(10, 2)
);`,
			checks: []struct{ name, kind string }{{"orders", "table"}},
		},
		{
			name: "create table schema-qualified",
			src: `CREATE TABLE analytics.orders (
    id INT,
    amount INT
);`,
			checks: []struct{ name, kind string }{{"orders", "table"}},
		},
		{
			name: "create table customers",
			src: `CREATE TABLE customers (
    customer_id BIGINT,
    email TEXT
);`,
			checks: []struct{ name, kind string }{{"customers", "table"}},
		},
		{
			name: "create table line items",
			src: `CREATE TABLE line_items (
    order_id BIGINT,
    sku TEXT,
    quantity INT
);`,
			checks: []struct{ name, kind string }{{"line_items", "table"}},
		},
		{
			name: "create index orders amount",
			src: `CREATE INDEX idx_orders_amount ON orders (amount);`,
			checks: []struct{ name, kind string }{{"idx_orders_amount", "index"}},
		},
		{
			name: "create index orders status",
			src: `CREATE INDEX idx_orders_status ON orders (status);`,
			checks: []struct{ name, kind string }{{"idx_orders_status", "index"}},
		},
		{
			name: "create function total orders",
			src: `CREATE FUNCTION total_orders() RETURNS BIGINT AS $$
    SELECT COUNT(*) FROM orders;
$$ LANGUAGE sql;`,
			checks: []struct{ name, kind string }{{"total_orders", "function"}},
		},
		{
			name: "create function identity",
			src: `CREATE FUNCTION identity_value(x INT) RETURNS INT AS $$
    SELECT x;
$$ LANGUAGE sql;`,
			checks: []struct{ name, kind string }{{"identity_value", "function"}},
		},
		{
			name: "create function public schema",
			src: `CREATE FUNCTION public.recalculate_totals() RETURNS INT AS $$
    SELECT 1;
$$ LANGUAGE sql;`,
			checks: []struct{ name, kind string }{{"recalculate_totals", "function"}},
		},
		{
			name: "create function two args",
			src: `CREATE FUNCTION calculate_tax(subtotal NUMERIC, rate NUMERIC) RETURNS NUMERIC AS $$
    SELECT subtotal * rate;
$$ LANGUAGE sql;`,
			checks: []struct{ name, kind string }{{"calculate_tax", "function"}},
		},
	}

	for _, vector := range vectors {
		t.Run(vector.name, func(t *testing.T) {
			result, err := ParseSource([]byte(vector.src), "conformance.sql", "sql", lang.Default.TreeSitter("sql"))
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Symbols) == 0 {
				debugParseResult(t, result)
				t.Fatal("expected at least one SQL symbol")
			}
			for _, want := range vector.checks {
				sym := findSymbolKind(result.Symbols, want.name, want.kind)
				if sym == nil {
					debugParseResult(t, result)
					t.Fatalf("expected %s %q", want.kind, want.name)
				}
				if sym.Language != "sql" {
					t.Fatalf("%s: expected sql language, got %q", want.name, sym.Language)
				}
			}
		})
	}
}
