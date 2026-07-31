package command

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func readPassword(cmd *cobra.Command, fromStdin bool) ([]byte, error) {
	if fromStdin {
		password, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("read password: %w", err)
		}
		return bytes.TrimRight([]byte(password), "\r\n"), nil
	}

	input, ok := cmd.InOrStdin().(*os.File)
	if !ok || !term.IsTerminal(int(input.Fd())) {
		return nil, errors.New("interactive password input requires a terminal; use --password-stdin")
	}

	fmt.Fprint(cmd.ErrOrStderr(), "Password: ")
	password, err := term.ReadPassword(int(input.Fd()))
	fmt.Fprintln(cmd.ErrOrStderr())
	if err != nil {
		return nil, fmt.Errorf("read password: %w", err)
	}

	fmt.Fprint(cmd.ErrOrStderr(), "Confirm password: ")
	confirmation, err := term.ReadPassword(int(input.Fd()))
	fmt.Fprintln(cmd.ErrOrStderr())
	if err != nil {
		return nil, fmt.Errorf("read password confirmation: %w", err)
	}
	if !bytes.Equal(password, confirmation) {
		return nil, errors.New("passwords do not match")
	}
	return password, nil
}
