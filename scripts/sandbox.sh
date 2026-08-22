#!/bin/sh
# ward sandbox — a repeatable dogfood scenario for a small agent session.
#
# Creates a scratch Go project with a live ward store and seeds the dispatch
# pool with work of varying floors, INCLUDING one task designed to fail so the
# escalation -> dossier path generates data too.
#
# Usage:
#   scripts/sandbox.sh [dir]     # default: /tmp/ward-sandbox-<timestamp>
#
# Then point the agent at the dir with one instruction:
#   "You are <name> with budget <tier>. Follow AGENTS.md exactly."
set -eu

DIR="${1:-/tmp/ward-sandbox-$(date +%s)}"
WARD_BIN="${WARD_BIN:-$(command -v ward || echo "$(pwd)/ward-bin")}"

if ! command -v "$WARD_BIN" >/dev/null 2>&1 && [ ! -x "$WARD_BIN" ]; then
  echo "error: ward binary not found (set WARD_BIN=/path/to/ward)" >&2
  exit 1
fi

mkdir -p "$DIR"
cd "$DIR"

# --- minimal Go project -----------------------------------------------------
cat > go.mod <<EOF
module sandbox/$$

go 1.24
EOF

cat > math.go <<'EOF'
package main

import "fmt"

// Add returns the sum of two ints.
func Add(a, b int) int { return a + b }

func main() { fmt.Println(Add(1, 2)) }
EOF

cat > math_test.go <<'EOF'
package main

import "testing"

func TestAdd(t *testing.T) {
	if Add(2, 3) != 5 {
		t.Fatal("2+3 must be 5")
	}
}
EOF

mkdir -p auth
cat > auth/auth.go <<'EOF'
package auth

// Login stubs an authentication check.
func Login(user, pass string) bool { return user != "" && pass != "" }
EOF
cat > auth/auth_test.go <<'EOF'
package auth

import "testing"

func TestLogin(t *testing.T) {
	if Login("", "x") {
		t.Fatal("empty user must fail")
	}
}
EOF

printf 'sandbox\n' > README.md

# --- ward store + protocol + scaffold ---------------------------------------
export WARD_HOME="$DIR/.ward"
"$WARD_BIN" init --scaffold

# --- seed the dispatch pool --------------------------------------------------
# Passing work (cheap/mid): exercises pull -> run -> capture -> done -> cheap reuse.
"$WARD_BIN" task add "run the full test suite" --tier cheap --kind test \
  --run "go test ./..." 
"$WARD_BIN" task add "check formatting is clean" --tier cheap \
  --run "test -z \"\$(gofmt -l .)\""
"$WARD_BIN" task add "verify build compiles" --tier mid --kind test \
  --run "go build ./..."
"$WARD_BIN" task add "confirm login guard rejects empty user" --tier mid --kind test \
  --run "grep -q 'user != ' auth/auth.go"
# Doomed work: fails at every tier -> escalation chain -> rejection + dossier.
"$WARD_BIN" task add "wire up payments provider (blocked on credentials)" --tier cheap \
  --run "test -f .payments-credentials"

echo
echo "sandbox ready: $DIR"
echo "store:         $WARD_HOME"
echo
echo "open tasks:"
"$WARD_BIN" task list
echo
echo "hand this directory to the agent with ONE line:"
echo '  "You are <agent-name>, budget <cheap|mid|strong>. cd here and follow AGENTS.md exactly."'
