package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"nyashachiroro.com/codex-account/internal/account"
	"nyashachiroro.com/codex-account/internal/app"
	"nyashachiroro.com/codex-account/internal/oauth"
	"nyashachiroro.com/codex-account/internal/platform"
	"nyashachiroro.com/codex-account/internal/version"
)

func Execute(ctx context.Context, args []string, in io.Reader, out, errW io.Writer, svc *app.Service) int {
	if svc == nil {
		built, err := defaultService(in, out, errW)
		if err != nil {
			fmt.Fprintf(errW, "Error: %s\n", err)
			return 1
		}
		svc = built
	}
	root := NewRoot(svc, in, out, errW)
	root.SetArgs(args)
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintf(errW, "Error: %s\n", err)
		return 1
	}
	return 0
}

func defaultService(in io.Reader, out, errW io.Writer) (*app.Service, error) {
	paths, err := platform.Resolve(platform.Env{})
	if err != nil {
		return nil, err
	}
	client := oauth.NewClient()
	client.Prompt = errW
	return app.New(app.Service{
		Paths:     paths,
		Refresher: client,
		OAuth:     client,
		Stdin:     in,
		Stdout:    out,
		Stderr:    errW,
	}), nil
}

func NewRoot(svc *app.Service, in io.Reader, out, errW io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:           "codex-account",
		Short:         "Save and switch one ChatGPT login for Pi, Codex, OpenCode, and Zed",
		Long:          longHelp,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	root.SetIn(in)
	root.SetOut(out)
	root.SetErr(errW)
	root.CompletionOptions.DisableDefaultCmd = false
	root.AddCommand(
		listCmd(svc),
		currentCmd(svc),
		saveCmd(svc),
		switchCmd(svc),
		loginCmd(svc),
		syncCmd(svc),
		refreshCmd(svc),
		usageCmd(svc),
		rmCmd(svc),
		versionCmd(),
	)
	return root
}

func listCmd(svc *app.Service) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List saved accounts. * is the live Pi login",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := svc.List(cmd.Context())
			if err != nil {
				return err
			}
			if len(res.Rows) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "(none)")
				return nil
			}
			for _, row := range res.Rows {
				prefix := " "
				switch {
				case row.LivePi:
					prefix = "*"
				case row.LiveCodex:
					prefix = "c"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", prefix, account.Heading(row.Name, row.Plan, row.Email))
			}
			return nil
		},
	}
}

func currentCmd(svc *app.Service) *cobra.Command {
	return &cobra.Command{
		Use:     "current",
		Aliases: []string{"status"},
		Short:   "Show live Pi, Codex, OpenCode, and Zed logins",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := svc.Current(cmd.Context())
			if err != nil {
				return err
			}
			for _, line := range res.Lines {
				fmt.Fprintln(cmd.OutOrStdout(), line)
			}
			fmt.Fprintln(cmd.OutOrStdout(), res.Shared)
			return nil
		},
	}
}

func saveCmd(svc *app.Service) *cobra.Command {
	var agent, name string
	var fromCodex, fromPi, fromOpenCode, fromZed bool
	cmd := &cobra.Command{
		Use:   "save",
		Short: "Snapshot live tokens into the shared store",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := takeName(name, args)
			if err != nil {
				return err
			}
			from, err := saveFrom(agent, fromCodex, fromPi, fromOpenCode, fromZed)
			if err != nil {
				return err
			}
			res, err := svc.Save(cmd.Context(), app.SaveOptions{From: from, Name: resolved})
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), res.Message)
			return nil
		},
	}
	bindAgentFlags(cmd, &agent)
	cmd.Flags().StringVarP(&name, "name", "n", "", "saved account name")
	cmd.Flags().BoolVar(&fromCodex, "codex", false, "alias for --from codex")
	cmd.Flags().BoolVar(&fromPi, "pi", false, "alias for --from pi")
	cmd.Flags().BoolVar(&fromOpenCode, "opencode", false, "alias for --from opencode")
	cmd.Flags().BoolVar(&fromZed, "zed", false, "alias for --from zed")
	cmd.Flags().Bool("from-pi", false, "alias for --from pi")
	cmd.Flags().Bool("from-opencode", false, "alias for --from opencode")
	cmd.Flags().Bool("from-zed", false, "alias for --from zed")
	_ = cmd.Flags().MarkHidden("from-pi")
	_ = cmd.Flags().MarkHidden("from-opencode")
	_ = cmd.Flags().MarkHidden("from-zed")
	cmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		if v, _ := cmd.Flags().GetBool("from-pi"); v {
			fromPi = true
		}
		if v, _ := cmd.Flags().GetBool("from-opencode"); v {
			fromOpenCode = true
		}
		if v, _ := cmd.Flags().GetBool("from-zed"); v {
			fromZed = true
		}
		return nil
	}
	return cmd
}

func switchCmd(svc *app.Service) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:               "switch [NAME]",
		Aliases:           []string{"use"},
		Short:             "Put a saved grant on Pi, Codex, OpenCode, and Zed",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeNames(svc),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := takeName(name, args)
			if err != nil {
				return err
			}
			if resolved == "" {
				return fmt.Errorf("usage: codex-account switch [-n|--name] NAME")
			}
			res, err := svc.Switch(cmd.Context(), resolved)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), res.Message)
			return nil
		},
	}
	cmd.Flags().StringVarP(&name, "name", "n", "", "saved account name")
	_ = cmd.RegisterFlagCompletionFunc("name", completeNames(svc))
	return cmd
}

func loginCmd(svc *app.Service) *cobra.Command {
	var agent, name string
	var device bool
	cmd := &cobra.Command{
		Use:   "login",
		Short: "ChatGPT OAuth via Pi or Codex, then write the grant to every tool",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := takeName(name, args)
			if err != nil {
				return err
			}
			if agent == "" {
				agent = "pi"
			}
			agent = strings.ToLower(agent)
			if agent != "pi" && agent != "codex" {
				return fmt.Errorf("login supports only 'pi' or 'codex'; the resulting grant is copied to every tool")
			}
			res, err := svc.Login(cmd.Context(), app.LoginOptions{Agent: agent, Device: device, Name: resolved})
			for _, note := range res.Notes {
				fmt.Fprintln(cmd.OutOrStdout(), note)
			}
			if err != nil {
				return err
			}
			if res.Message != "" {
				fmt.Fprintln(cmd.OutOrStdout(), res.Message)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&agent, "agent", "a", "", "login UI to use (pi or codex)")
	cmd.Flags().StringVarP(&name, "name", "n", "", "saved account name")
	cmd.Flags().BoolVar(&device, "device", false, "use device-code login")
	cmd.Flags().Bool("device-auth", false, "alias for --device")
	cmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		if v, _ := cmd.Flags().GetBool("device-auth"); v {
			device = true
		}
		return nil
	}
	_ = cmd.RegisterFlagCompletionFunc("agent", cobra.FixedCompletions([]string{"pi", "codex"}, cobra.ShellCompDirectiveNoFileComp))
	return cmd
}

func syncCmd(svc *app.Service) *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Copy the newer shared grant to all tools",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := svc.Sync(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), res.Message)
			return nil
		},
	}
}

func refreshCmd(svc *app.Service) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:               "refresh [NAME]",
		Short:             "Rotate tokens with OpenAI",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeNames(svc),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := takeName(name, args)
			if err != nil {
				return err
			}
			res, err := svc.Refresh(cmd.Context(), resolved)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), res.Message)
			return nil
		},
	}
	cmd.Flags().StringVarP(&name, "name", "n", "", "saved account name")
	_ = cmd.RegisterFlagCompletionFunc("name", completeNames(svc))
	return cmd
}

func usageCmd(svc *app.Service) *cobra.Command {
	var agent, name string
	var asJSON, all bool
	cmd := &cobra.Command{
		Use:               "usage",
		Aliases:           []string{"limits", "quota"},
		Short:             "Show ChatGPT Codex plan usage",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeNames(svc),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := takeName(name, args)
			if err != nil {
				return err
			}
			if agent != "" {
				agent = strings.ToLower(agent)
				if err := validAgent(agent, true); err != nil {
					return err
				}
			}
			rows, err := svc.Usage(cmd.Context(), app.UsageOptions{
				Agent: agent,
				Name:  resolved,
				All:   all,
				JSON:  asJSON,
			})
			if asJSON {
				text, jerr := app.FormatUsageJSON(rows)
				if jerr != nil {
					return jerr
				}
				fmt.Fprint(cmd.OutOrStdout(), text)
			} else if rows != nil {
				fmt.Fprint(cmd.OutOrStdout(), app.FormatUsageHuman(rows))
			}
			if errors.Is(err, app.ErrUsageFailed) {
				return err
			}
			return err
		},
	}
	bindAgentFlags(cmd, &agent)
	cmd.Flags().StringVarP(&name, "name", "n", "", "saved account name")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	cmd.Flags().BoolVar(&all, "all", false, "include every distinct workspace")
	_ = cmd.RegisterFlagCompletionFunc("name", completeNames(svc))
	return cmd
}

func rmCmd(svc *app.Service) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:               "rm [NAME]",
		Aliases:           []string{"remove", "delete"},
		Short:             "Delete a saved account. Live files are left alone",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeNames(svc),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := takeName(name, args)
			if err != nil {
				return err
			}
			if resolved == "" {
				return fmt.Errorf("usage: codex-account rm [-n|--name] NAME")
			}
			res, err := svc.Remove(cmd.Context(), resolved)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), res.Message)
			return nil
		},
	}
	cmd.Flags().StringVarP(&name, "name", "n", "", "saved account name")
	_ = cmd.RegisterFlagCompletionFunc("name", completeNames(svc))
	return cmd
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), version.Line())
			return nil
		},
	}
}

func bindAgentFlags(cmd *cobra.Command, agent *string) {
	cmd.Flags().StringVarP(agent, "agent", "a", "", "live login to read")
	cmd.Flags().StringVar(agent, "from", "", "live login to snapshot")
	_ = cmd.RegisterFlagCompletionFunc("agent", cobra.FixedCompletions([]string{"codex", "pi", "opencode", "zed"}, cobra.ShellCompDirectiveNoFileComp))
	_ = cmd.RegisterFlagCompletionFunc("from", cobra.FixedCompletions([]string{"codex", "pi", "opencode", "zed"}, cobra.ShellCompDirectiveNoFileComp))
}

func takeName(flagName string, args []string) (string, error) {
	name := flagName
	if name != "" && len(args) > 0 {
		return "", fmt.Errorf("unexpected argument %q", args[0])
	}
	if name == "" && len(args) > 0 {
		name = args[0]
	}
	if name == "" {
		return "", nil
	}
	name = account.NormalizeName(name)
	if err := account.ValidateName(name); err != nil {
		return "", err
	}
	return name, nil
}

func saveFrom(agent string, fromCodex, fromPi, fromOpenCode, fromZed bool) (string, error) {
	chosen := []string{}
	if fromCodex {
		chosen = append(chosen, "codex")
	}
	if fromPi {
		chosen = append(chosen, "pi")
	}
	if fromOpenCode {
		chosen = append(chosen, "opencode")
	}
	if fromZed {
		chosen = append(chosen, "zed")
	}
	if agent != "" {
		agent = strings.ToLower(agent)
		if err := validAgent(agent, true); err != nil {
			return "", err
		}
		chosen = append(chosen, agent)
	}
	seen := map[string]struct{}{}
	var uniq []string
	for _, c := range chosen {
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		uniq = append(uniq, c)
	}
	if len(uniq) > 1 {
		return "", fmt.Errorf("specify a single live source")
	}
	if len(uniq) == 1 {
		return uniq[0], nil
	}
	return "", nil
}

func validAgent(agent string, all bool) error {
	switch agent {
	case "codex", "pi":
		return nil
	case "opencode", "zed":
		if all {
			return nil
		}
		return fmt.Errorf("login supports only 'pi' or 'codex'; the resulting grant is copied to every tool")
	default:
		return fmt.Errorf("unknown agent %q. Use 'codex', 'pi', 'opencode', or 'zed'", agent)
	}
}

func completeNames(svc *app.Service) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		if svc == nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		var out []cobra.Completion
		for _, name := range svc.CompleteNames() {
			if toComplete == "" || strings.HasPrefix(name, toComplete) {
				out = append(out, cobra.Completion(name))
			}
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	}
}

const longHelp = `Save and switch one ChatGPT login for Pi, Codex, OpenCode, and Zed.

Pi owns login and is the usual source of truth; Codex, OpenCode, and Zed get converted copies.
Prefer this command instead of raw 'codex login'. Do not run 'codex logout' if you still want that saved account.

Codex credential storage must be file (the default), not keyring/auto/ephemeral.
Restart Pi/Codex/OpenCode/Zed after switching.`
