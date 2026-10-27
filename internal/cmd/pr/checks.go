package pr

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/cmdutil"
	"github.com/LeanMendez/prismatic-rainbow/internal/cmd/factory"
	"github.com/LeanMendez/prismatic-rainbow/internal/tableprinter"
)

// Exit codes of `pr checks`, mirroring gh: 1 when a check failed, 8 when
// checks are still running.
const (
	exitChecksFailed  = 1
	exitChecksPending = 8
)

func newCmdChecks(f *factory.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "checks [<pr>]",
		Short: "Estado de builds/pipelines del commit de origen del PR",
		Long: `Lista los build statuses (Pipelines, Jenkins, Bamboo, …) reportados sobre el
commit de origen del PR, normalizados en bucket: pass | fail | pending | stopped.

Exit code: 0 si no hay fallas ni pendientes, 1 si alguno falló, 8 si alguno
sigue corriendo. La salida se imprime siempre.`,
		Example: "  $ prismatic-rainbow pr checks 42 -o json",
		Args:    cmdutil.MaxArgs(1, "pr checks "+prArgUsage),
		RunE: func(_ *cobra.Command, args []string) error {
			client, t, err := resolve(f, args)
			if err != nil {
				return err
			}
			ss, err := client.ListPullRequestStatuses(t.Workspace, t.Repo, t.ID)
			if err != nil {
				return err
			}
			v := newChecksView(ss)
			if f.JSON() {
				err = cmdutil.PrintJSON(f.IO.Out, v)
			} else {
				err = writeChecksText(f, v)
			}
			if err != nil {
				return err
			}
			switch v.Overall {
			case BucketFail:
				return &cmdutil.ExitError{Code: exitChecksFailed}
			case BucketPending:
				return &cmdutil.ExitError{Code: exitChecksPending}
			}
			return nil
		},
	}
}

func writeChecksText(f *factory.Factory, v ChecksView) error {
	fmt.Fprintf(f.IO.Out, "%s · %d ok · %d fallidos · %d en curso · %d detenidos\n",
		strings.ToUpper(v.Overall), v.Passed, v.Failed, v.Pending, v.Stopped)
	tp := tableprinter.New(f.IO.Out, f.IO.IsStdoutTTY())
	for _, s := range v.Statuses {
		tp.AddRow(s.Bucket, cmdutil.Truncate(s.Name, 70), cmdutil.Truncate(s.Description, 40), s.URL)
	}
	return tp.Render()
}

func newCmdMergeability(f *factory.Factory) *cobra.Command {
	return &cobra.Command{
		Use:   "mergeability [<pr>]",
		Short: "¿Se puede mergear? Checks de aprobaciones, builds, tareas, conflictos y permisos",
		Long: `Consulta /mergeability/checks: estado del PR, permisos del usuario actual,
conflictos de git, merge checks del repo (aprobaciones mínimas, builds, tareas
resueltas, cambios pedidos) y checks custom (Forge).

mergeable_ignoring_permission ignora current_user_permission_check, útil para
agentes con tokens de solo lectura que quieren saber si el PR está listo.`,
		Example: "  $ prismatic-rainbow pr mergeability 42 -o json",
		Args:    cmdutil.MaxArgs(1, "pr mergeability "+prArgUsage),
		RunE: func(_ *cobra.Command, args []string) error {
			client, t, err := resolve(f, args)
			if err != nil {
				return err
			}
			cs, err := client.GetMergeabilityChecks(t.Workspace, t.Repo, t.ID)
			if err != nil {
				return err
			}
			v := newMergeabilityView(cs)
			if f.JSON() {
				return cmdutil.PrintJSON(f.IO.Out, v)
			}
			return writeMergeabilityText(f, v)
		},
	}
}

func writeMergeabilityText(f *factory.Factory, v MergeabilityView) error {
	switch {
	case v.Mergeable:
		fmt.Fprintln(f.IO.Out, f.IO.Green("Mergeable"))
	case v.MergeableIgnoringPermission:
		fmt.Fprintln(f.IO.Out, f.IO.Yellow("Listo para mergear (el usuario actual no tiene permiso de merge)"))
	default:
		fmt.Fprintf(f.IO.Out, "%s bloqueado por: %s\n", f.IO.Red("No mergeable:"), strings.Join(v.Blocking, ", "))
	}
	tp := tableprinter.New(f.IO.Out, f.IO.IsStdoutTTY())
	for _, c := range v.Checks {
		obs := ""
		if len(c.Observed) > 0 {
			obs = string(c.Observed)
		}
		flag := ""
		if c.Blocking {
			flag = "BLOQUEA"
		}
		tp.AddRow(c.Status, flag, c.Name, cmdutil.Truncate(c.Message, 60), obs)
	}
	return tp.Render()
}
