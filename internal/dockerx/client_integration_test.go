package dockerx

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/NorthernReach/contaigen/internal/model"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const integrationImage = "alpine:3.20"

func TestIntegrationEnvironmentLifecycle(t *testing.T) {
	requireIntegration(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	probe := startIntegrationProbe(t, ctx)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_ = probe.Terminate(cleanupCtx)
	})

	runtime, err := NewClient()
	if err != nil {
		t.Fatalf("create Docker client: %v", err)
	}
	t.Cleanup(func() {
		_ = runtime.Close()
	})

	envName := "it-" + randomHex(t, 4)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		timeout := 1
		_ = runtime.StopEnvironment(cleanupCtx, envName, &timeout)
		_ = runtime.RemoveEnvironment(cleanupCtx, envName, model.RemoveEnvironmentRequest{
			Force:         true,
			RemoveVolumes: true,
		})
	})

	env, warnings, err := runtime.CreateEnvironment(ctx, model.CreateEnvironmentRequest{
		Name:             envName,
		Image:            integrationImage,
		Shell:            "/bin/sh",
		NetworkProfile:   model.NetworkProfileBridge,
		NetworkMode:      model.NetworkProfileBridge,
		DisableWorkspace: true,
		Pull:             false,
	})
	if err != nil {
		t.Fatalf("create environment: %v", err)
	}
	if len(warnings) > 0 {
		t.Fatalf("unexpected create warnings: %#v", warnings)
	}
	if env.Name != envName || env.Image != integrationImage {
		t.Fatalf("unexpected created environment: %#v", env)
	}

	if err := runtime.StartEnvironment(ctx, envName); err != nil {
		t.Fatalf("start environment: %v", err)
	}
	inspected, err := runtime.InspectEnvironment(ctx, envName)
	if err != nil {
		t.Fatalf("inspect environment: %v", err)
	}
	if inspected.State != "running" {
		t.Fatalf("expected running environment, got %#v", inspected)
	}
	if inspected.Labels[model.LabelManaged] != "true" || inspected.Labels[model.LabelEnv] != envName {
		t.Fatalf("expected Contaigen labels, got %#v", inspected.Labels)
	}

	var stdout bytes.Buffer
	if err := runtime.EnterEnvironment(ctx, envName, model.EnterEnvironmentRequest{
		Command: []string{"sh", "-c", "printf contaigen-env-ok"},
	}, model.ExecIO{Stdout: &stdout}); err != nil {
		t.Fatalf("enter environment: %v", err)
	}
	if !strings.Contains(stdout.String(), "contaigen-env-ok") {
		t.Fatalf("unexpected exec output: %q", stdout.String())
	}
}

func requireIntegration(t *testing.T) {
	t.Helper()
	if os.Getenv("CONTAIGEN_INTEGRATION") != "1" {
		t.Skip("set CONTAIGEN_INTEGRATION=1 to run Docker integration tests")
	}
}

func startIntegrationProbe(t *testing.T, ctx context.Context) testcontainers.Container {
	t.Helper()

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image: integrationImage,
			Cmd: []string{
				"sh",
				"-c",
				"echo contaigen-testcontainers-ready && sleep 60",
			},
			Labels: map[string]string{
				"io.contaigen.integration": "testcontainers-probe",
			},
			WaitingFor: wait.ForLog("contaigen-testcontainers-ready").WithStartupTimeout(30 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("start testcontainers probe: %v", err)
	}
	return container
}

func randomHex(t *testing.T, bytesLen int) string {
	t.Helper()

	data := make([]byte, bytesLen)
	if _, err := rand.Read(data); err != nil {
		t.Fatalf("generate random suffix: %v", err)
	}
	return hex.EncodeToString(data)
}
