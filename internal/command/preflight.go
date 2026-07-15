package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"braid/internal/cli"
	"braid/internal/config"
	"braid/internal/gitexec"
)

type Git interface {
	RequireVersion(context.Context, string) error
	IsInsideWorkTree(context.Context) (bool, error)
	RelativeWorkingDir(context.Context) (string, error)
	WorkTreeRoot(context.Context) (string, error)
}

type RemoteGit interface {
	Git
	RemoteURL(context.Context, string) (string, bool, error)
	RemoteAdd(context.Context, string, string) error
	RemoteRemove(context.Context, string) error
}

type AddGit interface {
	RemoteGit
	RevParse(context.Context, string) (string, error)
	LsRemote(context.Context, ...string) (string, error)
	Fetch(context.Context, ...string) error
	LsTreeItem(context.Context, string, string) (gitexec.TreeItem, error)
	TreeContainsGitlink(context.Context, string, string) (bool, error)
	LsFiles(context.Context, string) (string, error)
	StatusPorcelainPathspecs(context.Context, ...string) (string, error)
	BlockingOperation(context.Context) (string, bool, error)
	Diff(context.Context, ...string) (string, error)
	HashBytes(context.Context, []byte) (gitexec.TreeItem, error)
	MakeTreeWithItemIn(context.Context, string, string, gitexec.TreeItem) (string, error)
	CommitTreeWithTemporaryIndex(context.Context, string, string) (bool, error)
	RestorePathspecsFromHead(context.Context, ...string) error
	RestorePathspecsFromTree(context.Context, string, bool, bool, ...string) error
}

type DiffGit interface {
	RemoteGit
	RevParse(context.Context, string) (string, error)
	Fetch(context.Context, ...string) error
	LsTreeItem(context.Context, string, string) (gitexec.TreeItem, error)
	TreeContainsGitlink(context.Context, string, string) (bool, error)
	MakeTreeWithItem(context.Context, string, gitexec.TreeItem) (string, error)
	Diff(context.Context, ...string) (string, error)
}

type StatusGit interface {
	DiffGit
	LsFiles(context.Context, string) (string, error)
}

type UpdateGit interface {
	StatusGit
	IgnoredPaths(context.Context, ...string) ([]string, error)
	MakeTreeWithItemIn(context.Context, string, string, gitexec.TreeItem) (string, error)
	MergeTrees(context.Context, map[string]string, string, string, string) (string, error)
	MergeTreeWrite(context.Context, string, string, string) (gitexec.MergeTreeResult, error)
	RepoFilePath(context.Context, string) (string, error)
	StatusPorcelainPathspecs(context.Context, ...string) (string, error)
	BlockingOperation(context.Context) (string, bool, error)
	Add(context.Context, string) error
	HashBytes(context.Context, []byte) (gitexec.TreeItem, error)
	HashFile(context.Context, string) (gitexec.TreeItem, error)
	CommitTreeWithTemporaryIndex(context.Context, string, string) (bool, error)
	RestorePathspecsFromHead(context.Context, ...string) error
	RestorePathspecsFromTree(context.Context, string, bool, bool, ...string) error
}

type RemoveGit interface {
	RemoteGit
	StatusPorcelainPathspecs(context.Context, ...string) (string, error)
	BlockingOperation(context.Context) (string, bool, error)
	Diff(context.Context, ...string) (string, error)
	HashBytes(context.Context, []byte) (gitexec.TreeItem, error)
	MakeTreeWithoutPath(context.Context, string, string) (string, error)
	MakeTreeWithItemIn(context.Context, string, string, gitexec.TreeItem) (string, error)
	CommitTreeWithTemporaryIndex(context.Context, string, string) (bool, error)
	RestorePathspecsFromHead(context.Context, ...string) error
	RestorePathspecsFromTree(context.Context, string, bool, bool, ...string) error
}

type UpgradeConfigGit interface {
	Git
	RevParse(context.Context, string) (string, error)
	UpdateRef(context.Context, ...string) error
	StatusPorcelainPathspecs(context.Context, ...string) (string, error)
	BlockingOperation(context.Context) (string, bool, error)
	HashBytes(context.Context, []byte) (gitexec.TreeItem, error)
	MakeTreeWithItemIn(context.Context, string, string, gitexec.TreeItem) (string, error)
	CommitTreeWithTemporaryIndex(context.Context, string, string) (bool, error)
	RestorePathspecsFromHead(context.Context, ...string) error
	RestorePathspecsFromTree(context.Context, string, bool, bool, ...string) error
}

type PushGit interface {
	UpdateGit
	ConfigGet(context.Context, ...string) (string, bool, error)
	CoreCommentChar(context.Context) (string, bool, error)
	FirstParentCommits(context.Context, string) ([]string, error)
	LogCommitsTouchingPath(context.Context, string, string) ([]gitexec.Commit, error)
	ShowFile(context.Context, string, string) ([]byte, bool, error)
	TreeItem(context.Context, string) (gitexec.TreeItem, error)
}

type Requirements struct {
	Git      bool
	Root     bool
	Config   bool
	MayWrite bool
}

type Options struct {
	Git        Git
	WorkDir    string
	ConfigRoot string
	Stdin      io.Reader
}

type Handler struct {
	Command cli.Command
	Options Options
}

type RepoContext struct {
	ProcessWorkDir      string
	GitWorkTreeRoot     string
	LogicalWorkTreeRoot string
	WorkTreePrefix      string
	RootGit             Git
	ProcessGit          Git
}

var ErrNotImplemented = errors.New("command is not implemented yet")

func NewApp() cli.App {
	return NewAppWithOptions(Options{})
}

func NewAppWithOptions(options Options) cli.App {
	app := cli.New()
	app.Handler = map[cli.Command]cli.Handler{
		cli.CommandAdd:           AddHandler{Options: options},
		cli.CommandPull:          UpdateHandler{Options: options},
		cli.CommandRemove:        RemoveHandler{Options: options},
		cli.CommandDiff:          DiffHandler{Options: options},
		cli.CommandPush:          PushHandler{Options: options},
		cli.CommandSync:          SyncHandler{Options: options},
		cli.CommandStatus:        StatusHandler{Options: options},
		cli.CommandCompletion:    CompletionHandler{Options: options},
		cli.CommandComplete:      CompleteHandler{Options: options},
		cli.CommandUpgradeConfig: UpgradeConfigHandler{Options: options},
	}
	return app
}

func (h Handler) Run(inv cli.Invocation, _, stderr io.Writer) error {
	if _, err := Preflight(context.Background(), h.Command, inv, h.Options, stderr); err != nil {
		return err
	}
	return fmt.Errorf("%s %w", h.Command, ErrNotImplemented)
}

func Preflight(ctx context.Context, command cli.Command, inv cli.Invocation, options Options, trace io.Writer) (RepoContext, error) {
	requirements := RequirementsFor(command)
	if !requirements.Git {
		repo, err := repoContextWithoutGit(options)
		return repo, err
	}

	repo, git, err := ResolveRepoContext(ctx, inv, options, trace)
	if err != nil {
		return RepoContext{}, err
	}

	if err := git.RequireVersion(ctx, gitexec.MinimumGitVersion); err != nil {
		return RepoContext{}, err
	}
	if requirements.Root {
		inside, err := git.IsInsideWorkTree(ctx)
		if err != nil {
			return RepoContext{}, errors.New("braid must run inside a git working tree")
		}
		if !inside {
			return RepoContext{}, errors.New("braid must run inside a git working tree")
		}
		if repo.WorkTreePrefix == "" {
			prefix, err := git.RelativeWorkingDir(ctx)
			if err != nil {
				return RepoContext{}, err
			}
			repo.WorkTreePrefix = cleanWorkTreePrefix(prefix)
		}
		if repo.GitWorkTreeRoot == "" {
			root, err := git.WorkTreeRoot(ctx)
			if err != nil {
				return RepoContext{}, err
			}
			repo.GitWorkTreeRoot = root
		}
		repo.LogicalWorkTreeRoot = logicalWorkTreeRoot(repo.ProcessWorkDir, repo.WorkTreePrefix)
		if repo.RootGit == nil && options.Git == nil {
			repo.RootGit = gitexec.New(repo.GitWorkTreeRoot, inv.Global.Verbose, trace)
		} else if repo.RootGit == nil {
			repo.RootGit = options.Git
		}
		if repo.ProcessGit == nil {
			repo.ProcessGit = git
		}
	}

	root := configRoot(options, repo)
	if requirements.Config {
		if err := requireConfigFile(root); err != nil {
			return RepoContext{}, err
		}
	}
	if command != cli.CommandUpgradeConfig {
		if _, err := config.Load(root); err != nil {
			return RepoContext{}, err
		}
	}

	return repo, nil
}

func ResolveRepoContext(ctx context.Context, inv cli.Invocation, options Options, trace io.Writer) (RepoContext, Git, error) {
	processWorkDir, err := absoluteWorkDir(options.WorkDir)
	if err != nil {
		return RepoContext{}, nil, err
	}

	processGit := options.Git
	if processGit == nil {
		processGit = gitexec.New(processWorkDir, inv.Global.Verbose, trace)
	}

	repo := RepoContext{
		ProcessWorkDir: processWorkDir,
		ProcessGit:     processGit,
	}

	inside, err := processGit.IsInsideWorkTree(ctx)
	if err != nil {
		return RepoContext{}, nil, err
	}
	if !inside {
		return repo, processGit, nil
	}
	prefix, err := processGit.RelativeWorkingDir(ctx)
	if err != nil {
		return RepoContext{}, nil, err
	}
	root, err := processGit.WorkTreeRoot(ctx)
	if err != nil {
		return RepoContext{}, nil, err
	}
	repo.GitWorkTreeRoot = root
	repo.WorkTreePrefix = cleanWorkTreePrefix(prefix)
	repo.LogicalWorkTreeRoot = logicalWorkTreeRoot(processWorkDir, repo.WorkTreePrefix)
	if options.Git != nil {
		repo.RootGit = options.Git
	} else {
		repo.RootGit = gitexec.New(root, inv.Global.Verbose, trace)
	}
	return repo, processGit, nil
}

func repoContextWithoutGit(options Options) (RepoContext, error) {
	processWorkDir, err := absoluteWorkDir(options.WorkDir)
	if err != nil {
		return RepoContext{}, err
	}
	return RepoContext{ProcessWorkDir: processWorkDir}, nil
}

func absoluteWorkDir(value string) (string, error) {
	if value == "" {
		return currentLogicalWorkDir()
	}
	dir := workDir(value)
	if filepath.IsAbs(dir) {
		return filepath.Clean(dir), nil
	}
	return filepath.Abs(dir)
}

func currentLogicalWorkDir() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if pwd := os.Getenv("PWD"); pwd != "" && filepath.IsAbs(pwd) {
		if sameDirectory(pwd, cwd) {
			return filepath.Clean(pwd), nil
		}
	}
	return cwd, nil
}

func sameDirectory(left, right string) bool {
	leftInfo, leftErr := os.Stat(left)
	rightInfo, rightErr := os.Stat(right)
	return leftErr == nil && rightErr == nil && os.SameFile(leftInfo, rightInfo)
}

func cleanWorkTreePrefix(prefix string) string {
	prefix = strings.ReplaceAll(prefix, `\`, "/")
	return strings.Trim(strings.TrimRight(prefix, "/"), "/")
}

func logicalWorkTreeRoot(processWorkDir, prefix string) string {
	processWorkDir = filepath.Clean(processWorkDir)
	prefix = cleanWorkTreePrefix(prefix)
	if prefix == "" {
		return processWorkDir
	}
	root := processWorkDir
	parts := strings.Split(prefix, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] == "" || parts[i] == "." {
			continue
		}
		if filepath.Base(root) != filepath.FromSlash(parts[i]) {
			return ""
		}
		next := filepath.Dir(root)
		if next == root {
			return ""
		}
		root = next
	}
	return root
}

func (r RepoContext) rootGit(inv cli.Invocation, options Options, trace io.Writer) Git {
	if options.Git != nil {
		return options.Git
	}
	if r.RootGit != nil {
		return r.RootGit
	}
	return gitexec.New(r.GitWorkTreeRoot, inv.Global.Verbose, trace)
}

func (r RepoContext) processGit(inv cli.Invocation, options Options, trace io.Writer) Git {
	if options.Git != nil {
		return options.Git
	}
	if r.ProcessGit != nil {
		return r.ProcessGit
	}
	return gitexec.New(r.ProcessWorkDir, inv.Global.Verbose, trace)
}

func RequirementsFor(command cli.Command) Requirements {
	switch command {
	case cli.CommandVersion, cli.CommandCompletion, cli.CommandComplete:
		return Requirements{}
	case cli.CommandStatus, cli.CommandDiff:
		return Requirements{Git: true, Root: true, Config: true}
	case cli.CommandAdd:
		return Requirements{Git: true, Root: true, MayWrite: true}
	case cli.CommandPull:
		return Requirements{Git: true, Root: true, Config: true, MayWrite: true}
	case cli.CommandRemove:
		return Requirements{Git: true, Root: true, Config: true, MayWrite: true}
	case cli.CommandUpgradeConfig:
		return Requirements{Git: true, Root: true, Config: true, MayWrite: true}
	case cli.CommandSync:
		return Requirements{Git: true, Root: true, Config: true, MayWrite: true}
	case cli.CommandPush:
		return Requirements{Git: true, Root: true, Config: true}
	default:
		return Requirements{}
	}
}

func requireConfigFile(root string) error {
	if _, err := os.Stat(filepath.Join(root, config.FileName)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("missing %s", config.FileName)
		}
		return err
	}
	return nil
}

func configRoot(options Options, repo RepoContext) string {
	if options.ConfigRoot != "" {
		return options.ConfigRoot
	}
	if repo.GitWorkTreeRoot != "" {
		return repo.GitWorkTreeRoot
	}
	return workDir(options.WorkDir)
}

func workDir(value string) string {
	if value == "" {
		return "."
	}
	return value
}
