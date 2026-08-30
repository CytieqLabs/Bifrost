package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/CytieqLabs/Bifrost/client/internal/api"
	"github.com/CytieqLabs/Bifrost/client/internal/backend"
	"github.com/CytieqLabs/Bifrost/client/internal/config"
	gitrepo "github.com/CytieqLabs/Bifrost/client/internal/git"
	"github.com/CytieqLabs/Bifrost/client/internal/service"
	bifrostv1 "github.com/CytieqLabs/Bifrost/protocol/v1"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "bifrost:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	global := flag.NewFlagSet("bifrost", flag.ContinueOnError)
	global.SetOutput(io.Discard)
	forceLocal := global.Bool("local", false, "use the embedded local backend")
	profileName := global.String("profile", "", "use a configured profile")
	if err := global.Parse(args); errors.Is(err, flag.ErrHelp) {
		usage()
		return nil
	} else if err != nil {
		return err
	}
	args = global.Args()
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		usage()
		return nil
	}
	if *forceLocal && *profileName != "" {
		return errors.New("use only one of --local or --profile")
	}
	if args[0] == "profile" {
		return profileCommand(args[1:])
	}
	if args[0] == "git" {
		return gitCommand(args[1:])
	}
	if isGitAlias(args[0]) {
		return gitCommand(args)
	}
	if args[0] == "init" || args[0] == "serve" {
		if *profileName != "" {
			return fmt.Errorf("%s always operates locally; remove --profile", args[0])
		}
		local, err := backend.OpenLocal(".")
		if err != nil {
			return err
		}
		if args[0] == "init" {
			return initCommand(local.Service)
		}
		return serveCommand(local.Service, args[1:])
	}
	selected, name, err := selectBackend(*forceLocal, *profileName)
	if err != nil {
		return err
	}
	ctx := context.Background()
	switch args[0] {
	case "status":
		return statusCommand(ctx, selected, name)
	case "run":
		return runCommand(ctx, selected, args[1:])
	case "checkpoint":
		return checkpointCommand(ctx, selected, args[1:])
	case "evidence":
		return evidenceCommand(ctx, selected, args[1:])
	case "workspace":
		return workspaceCommand(ctx, selected, args[1:])
	case "promotion":
		return promotionCommand(ctx, selected, args[1:])
	default:
		return fmt.Errorf("unknown command %q; run `bifrost help`", args[0])
	}
}

func selectBackend(forceLocal bool, requested string) (backend.Backend, string, error) {
	if forceLocal {
		local, err := backend.OpenLocal(".")
		return local, "local", err
	}
	store, err := config.DefaultStore()
	if err != nil {
		return nil, "", err
	}
	settings, err := store.Load()
	if err != nil {
		return nil, "", err
	}
	name := requested
	if name == "" {
		name = settings.Current
	}
	profile, ok := settings.Profiles[name]
	if !ok {
		return nil, "", fmt.Errorf("profile %q not found", name)
	}
	switch profile.Mode {
	case config.ModeLocal:
		local, err := backend.OpenLocal(".")
		return local, name, err
	case config.ModeRemote:
		token, err := config.ResolveToken(profile)
		if err != nil {
			return nil, "", err
		}
		remote, err := backend.OpenRemote(profile.Endpoint, token, nil)
		return remote, name, err
	default:
		return nil, "", fmt.Errorf("profile %q has unsupported mode %q", name, profile.Mode)
	}
}

func usage() {
	fmt.Print(`Bifrost Client — agent-native Git execution

Usage:
  bifrost [--local | --profile <name>] status
  bifrost [--local | --profile <name>] run start --task <text> --agent <name>
  bifrost [--local | --profile <name>] run list|show|finish ...
  bifrost [--local | --profile <name>] checkpoint create ...
  bifrost [--local | --profile <name>] evidence add ...
  bifrost [--local | --profile <name>] workspace list|path|remove ...
  bifrost [--local | --profile <name>] promotion check|apply ...
  bifrost git <git-arguments...>
  bifrost profile add|use|list|show|remove ...
  bifrost init
  bifrost serve [--addr 127.0.0.1:8741] [--token <token>|--token-file <path>]

Global backend flags must appear before the command.
`)
}

func isGitAlias(command string) bool {
	switch command {
	case "add", "commit", "diff", "fetch", "log", "pull", "push", "reset", "restore", "switch", "tag":
		return true
	default:
		return false
	}
}

func gitCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: bifrost git <git-arguments...>")
	}
	repo, err := gitrepo.Discover(".")
	if err != nil {
		return err
	}
	return repo.Run(args...)
}

func initCommand(app *service.Service) error {
	repository, err := app.Initialize()
	if err != nil {
		return err
	}
	head, _ := app.Repo.Head()
	fmt.Printf("initialized Bifrost at %s\nbase %s\n", filepath.Join(repository.Root, ".bifrost"), short(head))
	return nil
}

func statusCommand(ctx context.Context, app backend.Backend, profile string) error {
	status, err := app.Status(ctx)
	if err != nil {
		return err
	}
	worktree := "clean"
	if status.Dirty {
		worktree = "modified"
	}
	fmt.Printf("backend     %s (%s)\nrepository  %s\nhead        %s\nworktree    %s\nruns        %d (%d active)\ncheckpoints %d\nevidence    %d\npromotions  %d\n",
		app.Kind(), profile, status.Repository, short(status.Head), worktree, status.Runs, status.ActiveRuns, status.Checkpoints, status.Evidence, status.Promotions)
	return nil
}

func runCommand(ctx context.Context, app backend.Backend, args []string) error {
	if len(args) == 0 {
		return errors.New("run requires start, list, show, or finish")
	}
	switch args[0] {
	case "start":
		fs := flag.NewFlagSet("run start", flag.ContinueOnError)
		task := fs.String("task", "", "task objective")
		agent := fs.String("agent", "isha", "agent identity")
		parent := fs.String("parent", "", "parent run ID")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		run, err := app.StartRun(ctx, bifrostv1.StartRunRequest{Task: *task, Agent: *agent, ParentID: *parent})
		if err != nil {
			return err
		}
		printJSON(run)
		return nil
	case "list":
		runs, err := app.Runs(ctx)
		if err != nil {
			return err
		}
		for _, run := range runs {
			parent := ""
			if run.ParentID != "" {
				parent = " parent=" + run.ParentID
			}
			fmt.Printf("%-21s %-10s %-12s %s%s\n", run.ID, run.Status, run.Agent, run.Task, parent)
		}
		return nil
	case "show":
		if len(args) != 2 {
			return errors.New("usage: bifrost run show <run-id>")
		}
		details, err := app.Run(ctx, args[1])
		if err != nil {
			return err
		}
		printJSON(details)
		return nil
	case "finish":
		if len(args) < 2 {
			return errors.New("usage: bifrost run finish <run-id> [options]")
		}
		fs := flag.NewFlagSet("run finish", flag.ContinueOnError)
		status := fs.String("status", "completed", "completed, failed, or cancelled")
		result := fs.String("result", "", "result commit")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		run, err := app.FinishRun(ctx, args[1], bifrostv1.FinishRunRequest{Status: bifrostv1.RunStatus(*status), Result: *result})
		if err != nil {
			return err
		}
		fmt.Printf("%s %s result=%s\n", run.ID, run.Status, short(run.ResultCommit))
		return nil
	default:
		return fmt.Errorf("unknown run command %q", args[0])
	}
}

func checkpointCommand(ctx context.Context, app backend.Backend, args []string) error {
	if len(args) == 0 || args[0] != "create" {
		return errors.New("usage: bifrost checkpoint create --run <run-id> [--note <text>]")
	}
	fs := flag.NewFlagSet("checkpoint create", flag.ContinueOnError)
	runID := fs.String("run", "", "run ID")
	note := fs.String("note", "", "checkpoint note")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	checkpoint, err := app.CreateCheckpoint(ctx, *runID, bifrostv1.CreateCheckpointRequest{Note: *note})
	if err != nil {
		return err
	}
	printJSON(checkpoint)
	return nil
}

func evidenceCommand(ctx context.Context, app backend.Backend, args []string) error {
	if len(args) == 0 || args[0] != "add" {
		return errors.New("usage: bifrost evidence add --run <run-id> --kind <kind> [options]")
	}
	fs := flag.NewFlagSet("evidence add", flag.ContinueOnError)
	runID := fs.String("run", "", "run ID")
	kind := fs.String("kind", "", "test, build, scan, or review")
	command := fs.String("command", "", "executed command")
	exitCode := fs.Int("exit-code", 0, "command exit code")
	summary := fs.String("summary", "", "result summary")
	artifact := fs.String("artifact", "", "workspace-relative artifact path")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	evidence, err := app.AddEvidence(ctx, *runID, bifrostv1.AddEvidenceRequest{Kind: bifrostv1.EvidenceKind(*kind), Command: *command, ExitCode: *exitCode, Summary: *summary, Artifact: *artifact})
	if err != nil {
		return err
	}
	printJSON(evidence)
	return nil
}

func workspaceCommand(ctx context.Context, app backend.Backend, args []string) error {
	if len(args) == 0 {
		return errors.New("workspace requires list, path, or remove")
	}
	switch args[0] {
	case "list":
		runs, err := app.Runs(ctx)
		if err != nil {
			return err
		}
		for _, run := range runs {
			location := run.Workspace.Path
			if location == "" {
				location = "remote:" + run.Workspace.ID
			}
			fmt.Printf("%-21s %-10s %-8s %-8s %s\n", run.ID, run.Status, run.Workspace.Mode, run.Workspace.State, location)
		}
		return nil
	case "path":
		if len(args) != 2 {
			return errors.New("usage: bifrost workspace path <run-id>")
		}
		local, ok := app.(backend.LocalWorkspace)
		if !ok {
			return errors.New("remote workspaces do not expose server filesystem paths")
		}
		details, err := app.Run(ctx, args[1])
		if err != nil {
			return err
		}
		path := local.WorkspacePath(details.Run)
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("workspace unavailable: %w", err)
		}
		fmt.Println(path)
		return nil
	case "remove":
		if len(args) < 2 {
			return errors.New("usage: bifrost workspace remove <run-id> [--force]")
		}
		local, ok := app.(backend.LocalWorkspace)
		if !ok {
			return errors.New("remote workspace removal is managed by the server")
		}
		fs := flag.NewFlagSet("workspace remove", flag.ContinueOnError)
		force := fs.Bool("force", false, "remove a dirty or active workspace")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if err := local.RemoveWorkspace(ctx, args[1], *force); err != nil {
			return err
		}
		fmt.Printf("removed workspace for %s\n", args[1])
		return nil
	default:
		return fmt.Errorf("unknown workspace command %q", args[0])
	}
}

func promotionCommand(ctx context.Context, app backend.Backend, args []string) error {
	if len(args) == 0 {
		return errors.New("promotion requires check or apply")
	}
	switch args[0] {
	case "check":
		fs := flag.NewFlagSet("promotion check", flag.ContinueOnError)
		runID := fs.String("run", "", "completed run ID")
		target := fs.String("target", "HEAD", "target branch, ref, or commit")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		promotion, err := app.CheckPromotion(ctx, *runID, bifrostv1.CheckPromotionRequest{Target: *target})
		if err != nil {
			return err
		}
		printJSON(promotion)
		return nil
	case "apply":
		if len(args) != 2 {
			return errors.New("usage: bifrost promotion apply <promotion-id>")
		}
		promotion, err := app.ApplyPromotion(ctx, args[1])
		if err != nil {
			return err
		}
		printJSON(promotion)
		return nil
	default:
		return fmt.Errorf("unknown promotion command %q", args[0])
	}
}

func profileCommand(args []string) error {
	store, err := config.DefaultStore()
	if err != nil {
		return err
	}
	settings, err := store.Load()
	if err != nil {
		return err
	}
	if len(args) == 0 {
		return errors.New("profile requires add, use, list, show, or remove")
	}
	switch args[0] {
	case "add":
		if len(args) < 2 {
			return errors.New("usage: bifrost profile add <name> --mode local|remote [options]")
		}
		name := args[1]
		fs := flag.NewFlagSet("profile add", flag.ContinueOnError)
		mode := fs.String("mode", "local", "local or remote")
		endpoint := fs.String("endpoint", "", "remote Bifrost origin")
		tokenEnv := fs.String("token-env", "", "environment variable containing the token")
		tokenFile := fs.String("token-file", "", "absolute path containing the token")
		use := fs.Bool("use", false, "make this the current profile")
		replace := fs.Bool("replace", false, "replace an existing profile")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if _, exists := settings.Profiles[name]; exists && !*replace {
			return fmt.Errorf("profile %q already exists; use --replace", name)
		}
		profile := config.Profile{Mode: config.Mode(*mode), Endpoint: *endpoint, TokenEnv: *tokenEnv, TokenFile: *tokenFile}
		if err := config.ValidateProfile(profile); err != nil {
			return err
		}
		settings.Profiles[name] = profile
		if *use {
			settings.Current = name
		}
		if err := store.Save(settings); err != nil {
			return err
		}
		fmt.Printf("saved profile %s (%s)\n", name, profile.Mode)
		return nil
	case "use":
		if len(args) != 2 {
			return errors.New("usage: bifrost profile use <name>")
		}
		if _, ok := settings.Profiles[args[1]]; !ok {
			return fmt.Errorf("profile %q not found", args[1])
		}
		settings.Current = args[1]
		if err := store.Save(settings); err != nil {
			return err
		}
		fmt.Printf("using profile %s\n", settings.Current)
		return nil
	case "list":
		names := make([]string, 0, len(settings.Profiles))
		for name := range settings.Profiles {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			marker := " "
			if name == settings.Current {
				marker = "*"
			}
			profile := settings.Profiles[name]
			fmt.Printf("%s %-16s %-7s %s\n", marker, name, profile.Mode, profile.Endpoint)
		}
		return nil
	case "show":
		name := settings.Current
		if len(args) == 2 {
			name = args[1]
		} else if len(args) != 1 {
			return errors.New("usage: bifrost profile show [name]")
		}
		profile, ok := settings.Profiles[name]
		if !ok {
			return fmt.Errorf("profile %q not found", name)
		}
		printJSON(struct {
			Name string `json:"name"`
			config.Profile
		}{Name: name, Profile: profile})
		return nil
	case "remove":
		if len(args) != 2 {
			return errors.New("usage: bifrost profile remove <name>")
		}
		if args[1] == settings.Current {
			return errors.New("cannot remove the current profile; switch profiles first")
		}
		if _, ok := settings.Profiles[args[1]]; !ok {
			return fmt.Errorf("profile %q not found", args[1])
		}
		delete(settings.Profiles, args[1])
		if err := store.Save(settings); err != nil {
			return err
		}
		fmt.Printf("removed profile %s\n", args[1])
		return nil
	default:
		return fmt.Errorf("unknown profile command %q", args[0])
	}
}

func serveCommand(app *service.Service, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	address := fs.String("addr", "127.0.0.1:8741", "listen address")
	token := fs.String("token", os.Getenv("BIFROST_API_TOKEN"), "bearer token (or BIFROST_API_TOKEN)")
	tokenFile := fs.String("token-file", "", "read bearer token from a file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *tokenFile != "" {
		if *token != "" {
			return errors.New("use only one of --token, --token-file, or BIFROST_API_TOKEN")
		}
		data, err := os.ReadFile(*tokenFile)
		if err != nil {
			return fmt.Errorf("read token file: %w", err)
		}
		*token = strings.TrimSpace(string(data))
		if *token == "" {
			return errors.New("token file is empty")
		}
	}
	if err := api.ValidateAddress(*address, *token); err != nil {
		return err
	}
	if _, err := app.Status(); err != nil {
		return err
	}
	server := api.NewServer(*address, &api.Handler{Service: app, Token: *token})
	fmt.Printf("Bifrost API listening on http://%s/v1\n", *address)
	err := server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func printJSON(value any) {
	data, _ := json.MarshalIndent(value, "", "  ")
	fmt.Println(string(data))
}

func short(value string) string {
	if len(value) > 12 {
		return value[:12]
	}
	if value == "" {
		return "-"
	}
	return value
}
