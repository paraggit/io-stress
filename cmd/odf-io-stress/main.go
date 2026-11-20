package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
	"github.com/red-hat-storage/odf-io-stress/pkg/k8s"
	"github.com/red-hat-storage/odf-io-stress/pkg/worker"
	"github.com/red-hat-storage/odf-io-stress/pkg/workload"
	"github.com/spf13/cobra"
)

// version is set via -ldflags at release build time (e.g. -X main.version=v1.0.0).
var version = "dev"

func main() {
	rootCmd := newRootCmd()
	if err := rootCmd.Execute(); err != nil {
		os.Exit(exitCode(err))
	}
}

func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:     "odf-io-stress",
		Short:   "ODF IO stress testing tool for RBD and CephFS",
		Version: version,
	}
	// Don't dump the flags list on runtime failures (e.g. "N job(s) failed").
	rootCmd.SilenceUsage = true

	var (
		genOut   string
		genForce bool
	)

	runCmd := newRunCmd()

	genCmd := &cobra.Command{
		Use:          "generate-config",
		Short:        "Write a sample YAML config file",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return config.WriteSample(genOut, genForce)
		},
	}
	genCmd.Flags().StringVarP(&genOut, "output", "o", "odf-io-stress.yaml", "Output path (`-` for stdout)")
	genCmd.Flags().BoolVar(&genForce, "force", false, "Overwrite existing file")

	optCmd := &cobra.Command{
		Use:          "options",
		Short:        "Print every run and generate-config option as JSON",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			doc := config.OptionsDocument{
				SchemaVersion:  1,
				AppTypes:       config.ValidAppTypes(nil),
				Rules:          config.OptionRules(),
				Recipes:        config.Recipes(),
				Run:            config.CollectFlags(runCmd.Flags()),
				GenerateConfig: config.CollectFlags(genCmd.Flags()),
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetEscapeHTML(false)
			enc.SetIndent("", "  ")
			return enc.Encode(doc)
		},
	}

	var specPath string
	workerCmd := &cobra.Command{
		Use:          "s3-worker",
		Short:        "Run one object-workload phase inside the cluster",
		Hidden:       true,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, err := worker.LoadSpec(specPath)
			if err != nil {
				return err
			}
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			return worker.Run(ctx, spec, cmd.OutOrStdout())
		},
	}
	workerCmd.Flags().StringVar(&specPath, "spec", worker.SpecPath, "Path to the job spec JSON")

	exampleCmd := &cobra.Command{
		Use:                   "example",
		Short:                 "Print run examples",
		Long:                  "Print run examples. Does not touch the cluster.",
		Example:               "  odf-io-stress example workload\n  odf-io-stress example workload fio\n  odf-io-stress example workload object\n  odf-io-stress example fio\n  odf-io-stress example object",
		Args:                  cobra.NoArgs,
		DisableFlagsInUseLine: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	// Show the two real invocations. The default template only says "[command]".
	exampleCmd.SetUsageTemplate(`Usage:
{{.Example}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}

Available Commands:{{range $cmds}}{{if .IsAvailableCommand}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}
`)
	workloadCmd := &cobra.Command{
		Use:           "workload [fio|object]",
		Short:         "Print FIO or object run examples",
		Long:          "Print FIO or object run examples. Does not touch the cluster.",
		Example:       "  odf-io-stress example workload\n  odf-io-stress example workload fio\n  odf-io-stress example workload object",
		SilenceErrors: true,
		Args:          cobra.MaximumNArgs(1),
		ValidArgs:     []string{"fio", "object"},
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			return printWorkloadExamples(cmd.OutOrStdout(), name)
		},
	}
	exampleCmd.AddCommand(
		workloadCmd,
		newExampleCmd("fio", "Print FIO run examples"),
		newExampleCmd("object", "Print object run examples"),
	)

	rootCmd.AddCommand(runCmd, genCmd, optCmd, workerCmd, exampleCmd)
	return rootCmd
}

func newExampleCmd(name, short string) *cobra.Command {
	cmd := &cobra.Command{
		Use:                   name,
		Short:                 short,
		Long:                  short + ". Does not touch the cluster.",
		Args:                  cobra.NoArgs,
		DisableFlagsInUseLine: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return printWorkloadExamples(cmd.OutOrStdout(), name)
		},
	}
	// Do not inherit the parent usage template, which lists every workload.
	cmd.SetUsageTemplate(`Usage:
  {{.UseLine}}{{if .HasAvailableLocalFlags}}

Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}
`)
	return cmd
}

// plainUsageError is a human usage message. It is not an agent JSON summary.
type plainUsageError struct{ msg string }

func (e *plainUsageError) Error() string {
	if e == nil {
		return "invalid example"
	}
	return e.msg
}

func printWorkloadExamples(w io.Writer, name string) error {
	if name == "" {
		_, err := fmt.Fprintf(w, "# fio\n%s# object\n%s", workloadExamples["fio"], workloadExamples["object"])
		return err
	}
	text, ok := workloadExamples[name]
	if !ok {
		return &plainUsageError{msg: fmt.Sprintf("unknown workload %q\n\n  odf-io-stress example workload fio\n  odf-io-stress example workload object", name)}
	}
	_, err := fmt.Fprint(w, text)
	return err
}

// resolveDryRunObject discovers an RGW storage class or endpoint before
// manifests are printed. A NooBaa dry-run does not open a client.
func resolveDryRunObject(cfg *config.Config) error {
	if cfg == nil || !cfg.Tools.Object.Enabled || cfg.Tools.Object.Backend != config.BackendRGW {
		return nil
	}
	obj := cfg.Tools.Object
	var client *k8s.Client
	if strings.TrimSpace(obj.StorageClass) == "" || strings.TrimSpace(obj.Endpoint) == "" {
		c, err := k8s.NewClient(cfg.Cluster.Kubeconfig)
		if err != nil {
			return err
		}
		client = c
	}
	_, err := workload.ResolveObjectBackend(context.Background(), client, cfg)
	return err
}

// exitCode maps a run error to a process status an agent can branch on:
// 1 jobs failed, 2 config or usage, 3 cluster failure before a clean finish.
func exitCode(err error) int {
	return writeExit(os.Stdout, os.Stderr, err)
}

// writeExit prints a human message for example-command usage mistakes.
// Other usage errors stay agent JSON on stdout.
func writeExit(stdout, stderr io.Writer, err error) int {
	var exit *workload.ExitError
	if errors.As(err, &exit) {
		return exit.Code
	}
	var plain *plainUsageError
	if errors.As(err, &plain) {
		fmt.Fprintln(stderr, plain.Error())
		return 2
	}
	return workload.EmitUsage(stdout, err)
}
