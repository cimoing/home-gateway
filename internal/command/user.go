package command

import (
	"fmt"

	userservice "home-gateway/internal/user"

	"github.com/spf13/cobra"
)

func newUserCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "user",
		Short: "Manage users",
	}
	command.AddCommand(newUserCreateCommand(), newUserPasswordCommand())
	return command
}

func newUserCreateCommand() *cobra.Command {
	var passwordStdin bool
	command := &cobra.Command{
		Use:   "create <username>",
		Short: "Create a user",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			db, _, err := openDatabase(cmd.Context())
			if err != nil {
				return err
			}
			defer db.Close()

			password, err := readPassword(cmd, passwordStdin)
			if err != nil {
				return err
			}
			defer clearBytes(password)

			service := userservice.NewService(db)
			if err := service.Create(cmd.Context(), args[0], password); err != nil {
				return fmt.Errorf("create user %q: %w", args[0], err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "user %q created\n", args[0])
			return nil
		},
	}
	command.Flags().BoolVar(
		&passwordStdin,
		"password-stdin",
		false,
		"read one password line from standard input without confirmation",
	)
	return command
}

func newUserPasswordCommand() *cobra.Command {
	var passwordStdin bool
	command := &cobra.Command{
		Use:   "passwd <username>",
		Short: "Change a user's password",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			db, _, err := openDatabase(cmd.Context())
			if err != nil {
				return err
			}
			defer db.Close()

			password, err := readPassword(cmd, passwordStdin)
			if err != nil {
				return err
			}
			defer clearBytes(password)

			service := userservice.NewService(db)
			if err := service.UpdatePassword(cmd.Context(), args[0], password); err != nil {
				return fmt.Errorf("change password for %q: %w", args[0], err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "password updated for %q\n", args[0])
			return nil
		},
	}
	command.Flags().BoolVar(
		&passwordStdin,
		"password-stdin",
		false,
		"read one password line from standard input without confirmation",
	)
	return command
}

func clearBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
