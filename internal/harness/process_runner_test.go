package harness

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tjohnson/maestro/internal/config"
)

func TestNewProcessRunnerDefaultsToHost(t *testing.T) {
	runner, err := NewProcessRunner(nil)
	if err != nil {
		t.Fatalf("new process runner: %v", err)
	}
	if got := runner.Kind(); got != "host" {
		t.Fatalf("runner kind = %q, want host", got)
	}
}

func TestNewProcessRunnerRequiresDockerBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	_, err := NewProcessRunner(&config.DockerConfig{Image: "maestro-agent:latest"})
	if err == nil || !strings.Contains(err.Error(), "find docker executable") {
		t.Fatalf("new process runner error = %v, want docker lookup failure", err)
	}
}

func TestDockerProcessRunnerBuildsContainerCommand(t *testing.T) {
	tmp := t.TempDir()
	dockerBinary := filepath.Join(tmp, "docker")
	if err := os.WriteFile(dockerBinary, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write docker stub: %v", err)
	}
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ANTHROPIC_API_KEY", "claude-token")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "proxy-token")
	t.Setenv("ANTHROPIC_BASE_URL", "https://proxy.example.invalid")
	t.Setenv("DOCKER_HOST", "unix:///tmp/colima.sock")
	t.Setenv("DOCKER_CONTEXT", "colima")

	authDir := filepath.Join(tmp, "auth")
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		t.Fatalf("mkdir auth dir: %v", err)
	}
	cacheDir := filepath.Join(tmp, "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("mkdir cache dir: %v", err)
	}
	workdir := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	runner, err := NewProcessRunner(&config.DockerConfig{
		Image:              "maestro-agent:latest",
		WorkspaceMountPath: "/workspace",
		PullPolicy:         "always",
		Mounts: []config.DockerMountConfig{{
			Source:   authDir,
			Target:   "/tmp/maestro-home/.claude",
			ReadOnly: true,
		}},
		EnvPassthrough: []string{"ANTHROPIC_BASE_URL"},
		Network:        "none",
		CPUs:           1.5,
		Memory:         "2g",
		PIDsLimit:      128,
		Auth: &config.DockerAuthConfig{
			Mode: config.DockerAuthClaudeProxy,
		},
		Cache: &config.DockerCacheConfig{
			Profiles: []string{config.DockerCacheProfileGo},
			Mounts: []config.DockerCacheMountConfig{{
				Source: filepath.Join(cacheDir, "custom"),
				Target: "/cache/custom",
			}},
		},
	})
	if err != nil {
		t.Fatalf("new process runner: %v", err)
	}

	cmd, err := runner.CommandContext(context.Background(), ProcessSpec{
		Binary:  "claude",
		Args:    []string{"app-server", "--listen", "stdio://"},
		Workdir: workdir,
		Env: map[string]string{
			"EXPLICIT": "1",
		},
	})
	if err != nil {
		t.Fatalf("command context: %v", err)
	}

	args := strings.Join(cmd.Args, "\n")
	if !strings.Contains(args, dockerBinary) {
		t.Fatalf("args = %q, want docker binary path", args)
	}
	if !strings.Contains(args, "--mount\ntype=bind,src="+workdir+",dst=/workspace") {
		t.Fatalf("args = %q, want workspace mount", args)
	}
	if !strings.Contains(args, "--mount\ntype=bind,src="+authDir+",dst=/tmp/maestro-home/.claude,readonly") {
		t.Fatalf("args = %q, want readonly auth mount", args)
	}
	if !strings.Contains(args, "--workdir\n/workspace") {
		t.Fatalf("args = %q, want workdir /workspace", args)
	}
	if !strings.Contains(args, "--pull\nalways") {
		t.Fatalf("args = %q, want pull policy", args)
	}
	if !strings.Contains(args, "--network\nnone") {
		t.Fatalf("args = %q, want network none", args)
	}
	if !strings.Contains(args, "--cpus\n1.5") {
		t.Fatalf("args = %q, want cpu limit", args)
	}
	if !strings.Contains(args, "--memory\n2g") {
		t.Fatalf("args = %q, want memory limit", args)
	}
	if !strings.Contains(args, "--pids-limit\n128") {
		t.Fatalf("args = %q, want pid limit", args)
	}
	if !strings.Contains(args, "--security-opt\nno-new-privileges") {
		t.Fatalf("args = %q, want no-new-privileges security opt", args)
	}
	if !strings.Contains(args, "--read-only") {
		t.Fatalf("args = %q, want read-only rootfs", args)
	}
	if !strings.Contains(args, "--cap-drop\nALL") {
		t.Fatalf("args = %q, want cap drop", args)
	}
	if !strings.Contains(args, "--tmpfs\n/tmp") {
		t.Fatalf("args = %q, want tmpfs /tmp", args)
	}
	if !strings.Contains(args, "--env\nEXPLICIT=1") {
		t.Fatalf("args = %q, want explicit env", args)
	}
	if !strings.Contains(args, "--env\nANTHROPIC_BASE_URL=https://proxy.example.invalid") {
		t.Fatalf("args = %q, want env passthrough", args)
	}
	if !strings.Contains(args, "--env\nHOME="+defaultDockerHome) {
		t.Fatalf("args = %q, want default docker HOME", args)
	}
	if !strings.Contains(args, "--env\nANTHROPIC_AUTH_TOKEN=proxy-token") {
		t.Fatalf("args = %q, want auth env", args)
	}
	if !strings.Contains(args, "--mount\ntype=bind,src=") || !strings.Contains(args, ",dst="+defaultDockerHome) {
		t.Fatalf("args = %q, want writable docker home mount", args)
	}
	if !strings.Contains(args, "--mount\ntype=bind,src=") || !strings.Contains(args, ",dst="+filepath.Join(defaultDockerHome, ".cache", "go-build")) {
		t.Fatalf("args = %q, want go cache profile mount", args)
	}
	if !strings.Contains(args, "--mount\ntype=bind,src="+filepath.Join(cacheDir, "custom")+",dst=/cache/custom") {
		t.Fatalf("args = %q, want explicit cache mount", args)
	}
	if !strings.Contains(args, "maestro-agent:latest\nclaude\napp-server") {
		t.Fatalf("args = %q, want image and binary invocation", args)
	}
	if got := runner.VisibleWorkdir(workdir); got != "/workspace" {
		t.Fatalf("visible workdir = %q, want /workspace", got)
	}
	env := strings.Join(cmd.Env, "\n")
	if !strings.Contains(env, "DOCKER_HOST=unix:///tmp/colima.sock") {
		t.Fatalf("docker client env = %q, want DOCKER_HOST", env)
	}
	if !strings.Contains(env, "DOCKER_CONTEXT=colima") {
		t.Fatalf("docker client env = %q, want DOCKER_CONTEXT", env)
	}
}

func TestDockerProcessRunnerAppliesStructuredSecretsAndTools(t *testing.T) {
	tmp := t.TempDir()
	dockerBinary := filepath.Join(tmp, "docker")
	if err := os.WriteFile(dockerBinary, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write docker stub: %v", err)
	}
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ANTHROPIC_BASE_URL", "https://proxy.example.invalid")

	netrcPath := filepath.Join(tmp, "netrc")
	if err := os.WriteFile(netrcPath, []byte("machine example.invalid login test password secret\n"), 0o600); err != nil {
		t.Fatalf("write netrc: %v", err)
	}
	gitConfigPath := filepath.Join(tmp, "gitconfig")
	if err := os.WriteFile(gitConfigPath, []byte("[user]\n\tname = TJ\n"), 0o644); err != nil {
		t.Fatalf("write gitconfig: %v", err)
	}
	workdir := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	runner, err := NewProcessRunner(&config.DockerConfig{
		Image: "maestro-agent:latest",
		Secrets: &config.DockerSecretsConfig{
			Env: []config.DockerSecretEnvConfig{{
				Preset: config.DockerSecretEnvPresetAnthropicBaseURL,
			}},
			Mounts: []config.DockerAccessMountConfig{{
				Preset: config.DockerMountPresetNetrc,
				Source: netrcPath,
			}},
		},
		Tools: &config.DockerToolsConfig{
			Mounts: []config.DockerAccessMountConfig{{
				Preset: config.DockerMountPresetGitConfig,
				Source: gitConfigPath,
			}},
		},
	})
	if err != nil {
		t.Fatalf("new process runner: %v", err)
	}

	cmd, err := runner.CommandContext(context.Background(), ProcessSpec{
		Binary:  "claude",
		Args:    []string{"--version"},
		Workdir: workdir,
	})
	if err != nil {
		t.Fatalf("command context: %v", err)
	}

	args := strings.Join(cmd.Args, "\n")
	if !strings.Contains(args, "--env\nANTHROPIC_BASE_URL=https://proxy.example.invalid") {
		t.Fatalf("args = %q, want structured secret env", args)
	}
	if !strings.Contains(args, "--mount\ntype=bind,src="+netrcPath+",dst="+filepath.Join(defaultDockerHome, ".netrc")+",readonly") {
		t.Fatalf("args = %q, want structured secret mount", args)
	}
	if !strings.Contains(args, "--mount\ntype=bind,src="+gitConfigPath+",dst="+filepath.Join(defaultDockerHome, ".gitconfig")+",readonly") {
		t.Fatalf("args = %q, want structured tool mount", args)
	}
}

func TestDockerProcessRunnerBuildsAllowlistNetworkPolicyCommand(t *testing.T) {
	tmp := t.TempDir()
	dockerBinary := filepath.Join(tmp, "docker")
	if err := os.WriteFile(dockerBinary, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write docker stub: %v", err)
	}
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	workdir := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	runner, err := NewProcessRunner(&config.DockerConfig{
		Image: "maestro-agent:latest",
		NetworkPolicy: &config.DockerNetworkPolicyConfig{
			Mode:  config.DockerNetworkPolicyAllowlist,
			Allow: []string{"api.openai.com", "*.anthropic.com", "localhost"},
		},
	})
	if err != nil {
		t.Fatalf("new process runner: %v", err)
	}

	cmd, err := runner.CommandContext(context.Background(), ProcessSpec{
		Binary:  "codex",
		Args:    []string{"app-server"},
		Workdir: workdir,
	})
	if err != nil {
		t.Fatalf("command context: %v", err)
	}

	args := strings.Join(cmd.Args, "\n")
	if !strings.Contains(args, "--network\nbridge") {
		t.Fatalf("args = %q, want bridge network for allowlist mode", args)
	}
	if !strings.Contains(args, "--add-host\nhost.docker.internal:host-gateway") {
		t.Fatalf("args = %q, want host-gateway alias", args)
	}
	if !strings.Contains(args, "--env\nHTTP_PROXY=http://") || !strings.Contains(args, "@host.docker.internal:") {
		t.Fatalf("args = %q, want managed HTTP_PROXY", args)
	}
	if !strings.Contains(args, "--env\nHTTPS_PROXY=http://") {
		t.Fatalf("args = %q, want managed HTTPS_PROXY", args)
	}
	if !strings.Contains(args, "--env\nALL_PROXY=http://") {
		t.Fatalf("args = %q, want managed ALL_PROXY", args)
	}
	if !strings.Contains(args, "--env\nNO_PROXY=127.0.0.1,localhost,::1") {
		t.Fatalf("args = %q, want default NO_PROXY loopback exemptions", args)
	}
}

func TestDockerProcessRunnerCodexApiKeyWritesAuthHome(t *testing.T) {
	tmp := t.TempDir()
	dockerBinary := filepath.Join(tmp, "docker")
	if err := os.WriteFile(dockerBinary, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write docker stub: %v", err)
	}
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("OPENAI_API_KEY", "host-token")

	workdir := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	runner, err := NewProcessRunner(&config.DockerConfig{
		Image:      "maestro-agent:latest",
		Auth:       &config.DockerAuthConfig{Mode: config.DockerAuthCodexAPIKey},
		Cache:      &config.DockerCacheConfig{Profiles: []string{config.DockerCacheProfileCodex}},
		PullPolicy: "never",
	})
	if err != nil {
		t.Fatalf("new process runner: %v", err)
	}

	cmd, err := runner.CommandContext(context.Background(), ProcessSpec{
		Binary:  "codex",
		Args:    []string{"app-server"},
		Workdir: workdir,
	})
	if err != nil {
		t.Fatalf("command context: %v", err)
	}

	args := cmd.Args
	if !containsArgPair(args, "--pull", "never") {
		t.Fatalf("args = %q, want pull never", strings.Join(args, "\n"))
	}
	if !containsArgPair(args, "--env", "OPENAI_API_KEY=host-token") {
		t.Fatalf("args = %q, want codex api key env", strings.Join(args, "\n"))
	}
	homeSource, ok := mountSourceForTarget(args, defaultDockerHome)
	if !ok {
		t.Fatalf("args = %q, want writable home mount", strings.Join(args, "\n"))
	}
	body, err := os.ReadFile(filepath.Join(homeSource, ".codex", "auth.json"))
	if err != nil {
		t.Fatalf("read codex auth file: %v", err)
	}
	if got := string(body); !strings.Contains(got, `"OPENAI_API_KEY": "host-token"`) {
		t.Fatalf("auth.json = %q, want codex api key", got)
	}
	if !strings.Contains(strings.Join(args, "\n"), ",dst="+filepath.Join(defaultDockerHome, ".codex", "cache")) {
		t.Fatalf("args = %q, want codex cache profile mount", strings.Join(args, "\n"))
	}
}

func TestDockerProcessRunnerAppliesSecurityPresetCompat(t *testing.T) {
	tmp := t.TempDir()
	dockerBinary := filepath.Join(tmp, "docker")
	if err := os.WriteFile(dockerBinary, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write docker stub: %v", err)
	}
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	workdir := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	runner, err := NewProcessRunner(&config.DockerConfig{
		Image: "maestro-agent:latest",
		Security: &config.DockerSecurityConfig{
			Preset:         config.DockerSecurityPresetCompat,
			ReadOnlyRootFS: boolPtrHarnessTest(true),
		},
	})
	if err != nil {
		t.Fatalf("new process runner: %v", err)
	}

	cmd, err := runner.CommandContext(context.Background(), ProcessSpec{
		Binary:  "claude",
		Args:    []string{"--version"},
		Workdir: workdir,
	})
	if err != nil {
		t.Fatalf("command context: %v", err)
	}

	args := strings.Join(cmd.Args, "\n")
	if !strings.Contains(args, "--security-opt\nno-new-privileges") {
		t.Fatalf("args = %q, want no-new-privileges from compat preset", args)
	}
	if !strings.Contains(args, "--read-only") {
		t.Fatalf("args = %q, want read-only from explicit override", args)
	}
	if strings.Contains(args, "--cap-drop") {
		t.Fatalf("args = %q, want compat preset to omit cap drops", args)
	}
	if strings.Contains(args, "--tmpfs") {
		t.Fatalf("args = %q, want compat preset to omit tmpfs mounts", args)
	}
}

func TestDockerProcessRunnerCodexConfigMountUsesHomeDefaultTarget(t *testing.T) {
	tmp := t.TempDir()
	dockerBinary := filepath.Join(tmp, "docker")
	if err := os.WriteFile(dockerBinary, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write docker stub: %v", err)
	}
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	authDir := filepath.Join(tmp, "auth")
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		t.Fatalf("mkdir auth dir: %v", err)
	}
	workdir := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	runner, err := NewProcessRunner(&config.DockerConfig{
		Image: "maestro-agent:latest",
		Auth: &config.DockerAuthConfig{
			Mode:   config.DockerAuthCodexConfig,
			Source: authDir,
		},
	})
	if err != nil {
		t.Fatalf("new process runner: %v", err)
	}

	cmd, err := runner.CommandContext(context.Background(), ProcessSpec{
		Binary:  "codex",
		Args:    []string{"app-server"},
		Workdir: workdir,
	})
	if err != nil {
		t.Fatalf("command context: %v", err)
	}

	if !strings.Contains(strings.Join(cmd.Args, "\n"), "--mount\ntype=bind,src="+authDir+",dst="+filepath.Join(defaultDockerHome, ".codex")+",readonly") {
		t.Fatalf("args = %q, want codex config mount", strings.Join(cmd.Args, "\n"))
	}
}

func TestDockerProcessRunnerProfileKeyChangesWithWorkspaceMount(t *testing.T) {
	tmp := t.TempDir()
	dockerBinary := filepath.Join(tmp, "docker")
	if err := os.WriteFile(dockerBinary, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write docker stub: %v", err)
	}
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	workdirA := filepath.Join(tmp, "workspace-a")
	workdirB := filepath.Join(tmp, "workspace-b")
	if err := os.MkdirAll(workdirA, 0o755); err != nil {
		t.Fatalf("mkdir workspace a: %v", err)
	}
	if err := os.MkdirAll(workdirB, 0o755); err != nil {
		t.Fatalf("mkdir workspace b: %v", err)
	}

	runner, err := NewProcessRunner(&config.DockerConfig{
		Image: "maestro-agent:latest",
		Reuse: &config.DockerReuseConfig{Mode: config.DockerReuseModeStateless},
	})
	if err != nil {
		t.Fatalf("new process runner: %v", err)
	}
	dockerRunner := runner.(*dockerProcessRunner)

	keyA1, err := dockerRunner.profileKey(ProcessSpec{Binary: "codex", Workdir: workdirA}, defaultDockerHome)
	if err != nil {
		t.Fatalf("profile key a1: %v", err)
	}
	keyA2, err := dockerRunner.profileKey(ProcessSpec{Binary: "codex", Workdir: workdirA}, defaultDockerHome)
	if err != nil {
		t.Fatalf("profile key a2: %v", err)
	}
	keyB, err := dockerRunner.profileKey(ProcessSpec{Binary: "codex", Workdir: workdirB}, defaultDockerHome)
	if err != nil {
		t.Fatalf("profile key b: %v", err)
	}

	if keyA1 != keyA2 {
		t.Fatalf("profile keys for identical workspace differed: %q vs %q", keyA1, keyA2)
	}
	if keyA1 == keyB {
		t.Fatalf("profile keys should differ when workspace source changes: %q", keyA1)
	}
}

func TestDockerProcessRunnerStatelessReuseAcrossCompatibleRuns(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "docker.log")
	writeDockerStub(t, filepath.Join(tmp, "docker"), logPath, "")
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	manager, err := NewDockerReuseManager()
	if err != nil {
		t.Fatalf("new docker reuse manager: %v", err)
	}
	defer func() { _ = manager.Close() }()

	runner, err := NewProcessRunner(&config.DockerConfig{
		Image: "maestro-agent:latest",
		Reuse: &config.DockerReuseConfig{Mode: config.DockerReuseModeStateless},
	}, WithDockerReuseManager(manager))
	if err != nil {
		t.Fatalf("new process runner: %v", err)
	}

	lifecycle1 := &ProcessLifecycle{}
	cmd1, err := runner.CommandContext(context.Background(), ProcessSpec{
		RunID:     "run-1",
		Binary:    "codex",
		Args:      []string{"app-server"},
		Lifecycle: lifecycle1,
	})
	if err != nil {
		t.Fatalf("command context run 1: %v", err)
	}
	if !containsArgPair(cmd1.Args, "exec", "-i") {
		t.Fatalf("args = %q, want docker exec for reusable container", strings.Join(cmd1.Args, "\n"))
	}
	if lifecycle1.Metadata.ContainerReuse == nil || !lifecycle1.Metadata.ContainerReuse.Reused {
		t.Fatalf("metadata = %+v, want reused container metadata", lifecycle1.Metadata)
	}
	if err := lifecycle1.Release(context.Background(), nil); err != nil {
		t.Fatalf("release run 1: %v", err)
	}

	lifecycle2 := &ProcessLifecycle{}
	_, err = runner.CommandContext(context.Background(), ProcessSpec{
		RunID:     "run-2",
		Binary:    "codex",
		Args:      []string{"app-server"},
		Lifecycle: lifecycle2,
	})
	if err != nil {
		t.Fatalf("command context run 2: %v", err)
	}
	if lifecycle2.Metadata.ContainerReuse == nil || lifecycle2.Metadata.ContainerReuse.ContainerName != lifecycle1.Metadata.ContainerReuse.ContainerName {
		t.Fatalf("container reuse metadata mismatch: run1=%+v run2=%+v", lifecycle1.Metadata, lifecycle2.Metadata)
	}
	if err := lifecycle2.Release(context.Background(), nil); err != nil {
		t.Fatalf("release run 2: %v", err)
	}

	logBody, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read docker log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(logBody)), "\n")
	if countLogLines(lines, "create ") != 1 {
		t.Fatalf("docker log = %q, want one create", string(logBody))
	}
	if countLogLines(lines, "start ") != 2 {
		t.Fatalf("docker log = %q, want two starts", string(logBody))
	}
}

func TestDockerProcessRunnerLineageReuseStaysScoped(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "docker.log")
	writeDockerStub(t, filepath.Join(tmp, "docker"), logPath, "")
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	manager, err := NewDockerReuseManager()
	if err != nil {
		t.Fatalf("new docker reuse manager: %v", err)
	}
	defer func() { _ = manager.Close() }()

	runner, err := NewProcessRunner(&config.DockerConfig{
		Image: "maestro-agent:latest",
		Reuse: &config.DockerReuseConfig{Mode: config.DockerReuseModeLineage},
	}, WithDockerReuseManager(manager))
	if err != nil {
		t.Fatalf("new process runner: %v", err)
	}

	lifecycle1 := &ProcessLifecycle{}
	if _, err := runner.CommandContext(context.Background(), ProcessSpec{
		RunID:      "run-1",
		LineageKey: "source=a|issue=1|workspace=/tmp/a",
		Binary:     "codex",
		Args:       []string{"app-server"},
		Lifecycle:  lifecycle1,
	}); err != nil {
		t.Fatalf("command context lineage 1: %v", err)
	}
	_ = lifecycle1.Release(context.Background(), nil)

	lifecycle2 := &ProcessLifecycle{}
	if _, err := runner.CommandContext(context.Background(), ProcessSpec{
		RunID:      "run-2",
		LineageKey: "source=a|issue=2|workspace=/tmp/b",
		Binary:     "codex",
		Args:       []string{"app-server"},
		Lifecycle:  lifecycle2,
	}); err != nil {
		t.Fatalf("command context lineage 2: %v", err)
	}
	_ = lifecycle2.Release(context.Background(), nil)

	if lifecycle1.Metadata.ContainerReuse == nil || lifecycle2.Metadata.ContainerReuse == nil {
		t.Fatalf("missing container reuse metadata: run1=%+v run2=%+v", lifecycle1.Metadata, lifecycle2.Metadata)
	}
	if lifecycle1.Metadata.ContainerReuse.ContainerName == lifecycle2.Metadata.ContainerReuse.ContainerName {
		t.Fatalf("lineage reuse should not cross lineages: %q", lifecycle1.Metadata.ContainerReuse.ContainerName)
	}
}

func TestDockerProcessRunnerFallsBackToColdRunWhenReusableBusy(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "docker.log")
	writeDockerStub(t, filepath.Join(tmp, "docker"), logPath, "")
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	manager, err := NewDockerReuseManager()
	if err != nil {
		t.Fatalf("new docker reuse manager: %v", err)
	}
	defer func() { _ = manager.Close() }()

	runner, err := NewProcessRunner(&config.DockerConfig{
		Image: "maestro-agent:latest",
		Reuse: &config.DockerReuseConfig{Mode: config.DockerReuseModeStateless},
	}, WithDockerReuseManager(manager))
	if err != nil {
		t.Fatalf("new process runner: %v", err)
	}

	lifecycle1 := &ProcessLifecycle{}
	_, err = runner.CommandContext(context.Background(), ProcessSpec{
		RunID:     "run-1",
		Binary:    "codex",
		Args:      []string{"app-server"},
		Lifecycle: lifecycle1,
	})
	if err != nil {
		t.Fatalf("command context run 1: %v", err)
	}

	lifecycle2 := &ProcessLifecycle{}
	cmd2, err := runner.CommandContext(context.Background(), ProcessSpec{
		RunID:     "run-2",
		Binary:    "codex",
		Args:      []string{"app-server"},
		Lifecycle: lifecycle2,
	})
	if err != nil {
		t.Fatalf("command context run 2: %v", err)
	}
	if slices.Contains(cmd2.Args, "exec") {
		t.Fatalf("args = %q, want cold docker run fallback while reusable container is busy", strings.Join(cmd2.Args, "\n"))
	}
	if lifecycle2.Metadata.ContainerReuse == nil || lifecycle2.Metadata.ContainerReuse.Reused {
		t.Fatalf("metadata = %+v, want non-reused cold fallback metadata", lifecycle2.Metadata)
	}
	dockerRunner := runner.(*dockerProcessRunner)
	sharedHome, err := dockerRunner.prepareHomeSource(
		defaultDockerHome,
		config.DockerReuseModeStateless,
		lifecycle1.Metadata.ContainerReuse.ProfileKey,
		lifecycle1.Metadata.ContainerReuse.LineageKey,
	)
	if err != nil {
		t.Fatalf("prepare shared home: %v", err)
	}
	coldHome, ok := mountSourceForTarget(cmd2.Args, defaultDockerHome)
	if !ok {
		t.Fatalf("args = %q, want cold fallback HOME mount", strings.Join(cmd2.Args, "\n"))
	}
	if coldHome == sharedHome {
		t.Fatalf("cold fallback HOME mount = %q, want fresh temp HOME distinct from shared reuse home %q", coldHome, sharedHome)
	}

	_ = lifecycle1.Release(context.Background(), nil)
}

func TestDockerReuseManagerPrunesOrphanedContainers(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "docker.log")
	stalePID := "999999"
	writeDockerStub(t, filepath.Join(tmp, "docker"), logPath, stalePID)
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	manager, err := NewDockerReuseManager()
	if err != nil {
		t.Fatalf("new docker reuse manager: %v", err)
	}
	defer func() { _ = manager.Close() }()

	if err := manager.pruneOrphans(context.Background()); err != nil {
		t.Fatalf("prune orphans: %v", err)
	}

	logBody, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read docker log: %v", err)
	}
	if !strings.Contains(string(logBody), "rm -f stale-container") {
		t.Fatalf("docker log = %q, want orphan cleanup", string(logBody))
	}
}

func TestWriteCodexAuthHome(t *testing.T) {
	home, err := writeCodexAuthHome("sk-test")
	if err != nil {
		t.Fatalf("write codex auth home: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(home, ".codex", "auth.json"))
	if err != nil {
		t.Fatalf("read auth file: %v", err)
	}
	if got := string(body); !strings.Contains(got, `"auth_mode": "apikey"`) || !strings.Contains(got, `"OPENAI_API_KEY": "sk-test"`) {
		t.Fatalf("auth.json = %q, want api auth payload", got)
	}
}

func containsArgPair(args []string, key, value string) bool {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == key && args[i+1] == value {
			return true
		}
	}
	return false
}

func boolPtrHarnessTest(value bool) *bool {
	v := value
	return &v
}

func mountSourceForTarget(args []string, target string) (string, bool) {
	for i := 0; i < len(args)-1; i++ {
		if args[i] != "--mount" {
			continue
		}
		spec := args[i+1]
		if !strings.Contains(spec, ",dst="+target) {
			continue
		}
		for _, segment := range strings.Split(spec, ",") {
			if strings.HasPrefix(segment, "src=") {
				return strings.TrimPrefix(segment, "src="), true
			}
		}
	}
	return "", false
}

func writeDockerStub(t *testing.T, path string, logPath string, stalePID string) {
	t.Helper()
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> \"" + logPath + "\"\n" +
		"case \"$1\" in\n" +
		"  create)\n" +
		"    echo container-123\n" +
		"    ;;\n" +
		"  ps)\n"
	if stalePID != "" {
		script += "    echo \"stale-container\t" + stalePID + "\"\n"
	}
	script += "    ;;\n" +
		"  start|stop|rm|exec)\n" +
		"    ;;\n" +
		"  *)\n" +
		"    ;;\n" +
		"esac\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write docker stub: %v", err)
	}
}

func countLogLines(lines []string, prefix string) int {
	count := 0
	for _, line := range lines {
		if strings.HasPrefix(line, prefix) {
			count++
		}
	}
	return count
}
