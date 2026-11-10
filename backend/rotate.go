package backend

// Vault-key rotation entry points for the binary's subcommands; the
// mechanics live in internal/vault.
import (
	"bufio"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/radch-ai/ava/backend/internal/vault"
)

// MasterKeyFromHex parses the master-key wire form shared by env
// binding and the rotation prompt.
func MasterKeyFromHex(s string) ([]byte, error) {
	return vault.MasterKeyFromHex(s)
}

// PromptNewMasterKey asks for the new master key twice and returns it
// parsed — entries hidden when in is a terminal, plain lines when
// piped (tests, automation). Three attempts; closed input aborts.
func PromptNewMasterKey(in io.Reader, out io.Writer) ([]byte, error) {
	lines := bufio.NewScanner(in)
	read := func(prompt string) (string, error) {
		_, _ = fmt.Fprint(out, prompt)
		if f, ok := in.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
			line, err := term.ReadPassword(int(f.Fd()))
			_, _ = fmt.Fprintln(out)
			return strings.TrimSpace(string(line)), err
		}
		if !lines.Scan() {
			return "", errors.New("input closed")
		}
		return strings.TrimSpace(lines.Text()), nil
	}

	for range 3 {
		first, err := read("New master key (64 hex chars): ")
		if err != nil {
			return nil, err
		}
		if first == "" {
			_, _ = fmt.Fprintln(out, "empty entry")
			continue
		}
		key, perr := vault.MasterKeyFromHex(first)
		if perr != nil {
			_, _ = fmt.Fprintln(out, perr)
			continue
		}
		confirm, err := read("Confirm: ")
		if err != nil {
			return nil, err
		}
		if first != confirm {
			_, _ = fmt.Fprintln(out, "entries do not match")
			continue
		}
		return key, nil
	}
	return nil, errors.New("master key not provided after 3 attempts")
}

// RotateMaster re-wraps the vault keyset from oldKey to newKey. The
// entry keys do not change — no stored secret is re-sealed.
func RotateMaster(sqlDB *sql.DB, oldKey, newKey []byte) error {
	oldMaster, err := vault.NewMasterAEAD(oldKey)
	if err != nil {
		return err
	}
	newMaster, err := vault.NewMasterAEAD(newKey)
	if err != nil {
		return err
	}
	return vault.RotateMaster(sqlDB, oldMaster, newMaster)
}

// RotateKeys adds and promotes a fresh vault data key and re-seals
// every stored entry under it, whole or not at all. Returns the number
// of re-sealed entries.
func RotateKeys(sqlDB *sql.DB, masterKey []byte) (int, error) {
	master, err := vault.NewMasterAEAD(masterKey)
	if err != nil {
		return 0, err
	}
	return vault.RotateKeys(sqlDB, master)
}
