package powershelltools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"ccgo/internal/contracts"
	"ccgo/internal/tool"
)

func powerShellExecutor(t *testing.T) tool.Executor {
	t.Helper()
	registry, err := tool.NewRegistry(NewPowerShellTool(), NewPowerShellOutputTool(), NewKillPowerShellTool())
	if err != nil {
		t.Fatal(err)
	}
	return tool.NewExecutor(registry)
}

func TestPowerShellValidation(t *testing.T) {
	executor := powerShellExecutor(t)
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty command", input: `{"command":"  "}`, want: "command is required"},
		{name: "invalid timeout", input: `{"command":"Get-Location","timeout":0}`, want: "timeout must be positive"},
		{name: "unknown field", input: `{"command":"Get-Location","extra":true}`, want: "input.extra is not allowed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := executor.Execute(tool.Context{Context: context.Background(), Metadata: map[string]any{}}, contracts.ToolUse{
				ID:    "toolu_invalid",
				Name:  "PowerShell",
				Input: json.RawMessage(tt.input),
			}, nil)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestPowerShellCommandClassification(t *testing.T) {
	readOnly := []string{
		"Get-Location",
		"pwd",
		"Get-ChildItem -Force",
		"Get-ChildItem -Filter '*.go' ./internal",
		"Get-ChildItem -Depth 2 ./internal",
		"Get-ChildItem ./internal",
		"Get-Content README.md",
		"Get-Content -Raw README.md",
		"Get-Content -Encoding UTF8 README.md",
		"Get-Content -ReadCount 0 README.md",
		`Get-Content .\README.md`,
		"type README.md",
		"gi README.md",
		"gp . -Name Mode",
		"Select-String TODO README.md",
		"Select-String -Pattern TODO -Path README.md",
		"Select-String -SimpleMatch TODO README.md",
		"ConvertTo-Json -InputObject hello -Depth 2",
		`ConvertFrom-Json '{"a":1}'`,
		"ConvertTo-Csv -InputObject value -NoTypeInformation",
		"ConvertFrom-Csv -Header Name one",
		"ConvertTo-Xml -InputObject value -Depth 1",
		"ConvertTo-Html -Title report -Fragment",
		"Get-Member -Name Length",
		"Get-Unique -AsString value",
		"Compare-Object -ReferenceObject a -DifferenceObject b",
		"Join-String -Separator , a b",
		"Get-Random -Minimum 1 -Maximum 10",
		"Convert-Path README.md",
		"Get-ItemProperty . -Name Mode",
		"Get-ItemPropertyValue . -Name Mode",
		"Get-PSProvider -PSProvider FileSystem",
		"Get-Process | Select-String go",
		"Get-Process | Select-Object Name",
		"Get-Process | Select-Object -First 5 Name",
		"Get-Process | Sort-Object Name",
		"Get-Process | Group-Object Name",
		"Get-Process | Where-Object Name -EQ go",
		"Get-Process | Format-Table -AutoSize Name",
		"Get-Process | Format-List Name",
		"Get-Process | Format-Wide Name",
		"Get-Process | Format-Custom Name",
		"Get-Process | Measure-Object -Property CPU",
		"Get-Process | Out-String -Width 120",
		"Get-Process | Out-Host -Paging",
		"Get-Process -Name go",
		"ps -Name go",
		"Get-Service -Name ssh-agent",
		"gsv -Name ssh-agent",
		"Get-Date -Format o",
		"Get-PSDrive -Name C",
		"Get-Module -ListAvailable",
		"Get-Alias -Name ls",
		"Get-History -Count 5",
		"h -Count 5",
		"Get-TimeZone -ListAvailable",
		"Get-ComputerInfo",
		"Get-Host",
		"Get-Culture",
		"Get-UICulture",
		"Get-Uptime",
		"Get-NetAdapter -Physical",
		"Get-NetAdapter -Name Ethernet",
		"Get-NetIPAddress -AddressFamily IPv4",
		"Get-NetIPConfiguration -Detailed",
		"Get-NetRoute -DestinationPrefix 0.0.0.0/0",
		"Get-DnsClientCache -Name example.com",
		"Get-DnsClient -InterfaceAlias Ethernet",
		"Get-EventLog -LogName System -Newest 5",
		"Get-WinEvent -LogName System -MaxEvents 5",
		"Get-WinEvent -Path logs/system.evtx",
		"Get-CimClass -ClassName Win32_Process",
		"ipconfig /all",
		"netstat -ano",
		"systeminfo /FO CSV /NH",
		"tasklist /V /FO CSV",
		"where.exe git",
		"hostname -s",
		"whoami /all /fo csv",
		"ver",
		"arp -a",
		"route print",
		"route -4 print",
		"getmac /V /FO CSV",
		"file --mime-type README.md",
		"tree /F /A .",
		"findstr /I TODO README.md",
		"findstr /C:TODO README.md",
		"dotnet --info",
		"dotnet --list-runtimes --list-sdks",
		"docker ps",
		"docker images",
		"docker logs --tail 50 app",
		"docker logs -n 10 --timestamps app",
		"docker inspect --format '{{.Id}}' app",
		"docker inspect --type container --size app",
		"Start-Sleep -Milliseconds 1",
		"sleep 1",
		"Write-Output hello",
		"Write-Output '`'",
		"Write-Output -InputObject hello",
		"Write-Host -ForegroundColor Red hello",
		"git status --short",
		"git.exe diff --stat -- README.md",
		"git.cmd log --oneline --max-count 2",
	}
	for _, command := range readOnly {
		if !IsReadOnlyCommand(command) {
			t.Fatalf("%q should be read-only", command)
		}
		if IsDestructiveCommand(command) {
			t.Fatalf("%q should not be destructive", command)
		}
	}

	notReadOnly := []string{
		"Get-Content $env:USERPROFILE",
		"Get-Content README.md > out.txt",
		"$x = Get-Process",
		"Get-Location; Set-Location ..",
		"Get-Location && Get-Process",
		"Get-Content /etc/passwd",
		"Get-Content ../secret.txt",
		`Get-Content C:\Users\secret.txt`,
		`Get-Content \\server\share\secret.txt`,
		"Get-ChildItem ~",
		`Get-Item HKLM:\Software`,
		"Get-Content '$HOME/file'",
		"Select-String TODO /etc/passwd",
		"Get-Content --% README.md",
		"Get-Content 'README.md",
		"Get-Content \"README.md",
		"Write-Output hello `",
		"Get-Content 'README.md; Remove-Item out.txt",
		"Get-Content -Credential admin README.md",
		"Get-Content -Raw:unexpected README.md",
		"Get-Content -Encoding",
		"Get-ChildItem -Credential admin ./internal",
		"Select-String -Pattern TODO -Path /etc/passwd",
		"Get-Process -ComputerName server",
		"Write-Output -OutFile out.txt hello",
		"Get-Command -Name git",
		"Get-Help Get-Content",
		"Get-Clipboard",
		`Select-Xml -Content '<x />' -XPath /`,
		`Test-Json '{}'`,
		"ConvertTo-Json -OutFile out.json hello",
		"Get-Module -CimSession server",
		"Get-Date -ComputerName server",
		"Start-Sleep -Seconds $env:SECRET",
		`Get-ItemProperty HKLM:\Software -Name Path`,
		"Get-Process | Select-Object $env:SECRET",
		"Get-Process | Select-Object @{N='x';E={Remove-Item out.txt}}",
		"Get-Process | Where-Object { Remove-Item out.txt }",
		"Get-Process | Format-Table -Property:$env:SECRET",
		"Get-Process | Out-String -InputObject:$env:SECRET",
		"Write-Output `$env:SECRET",
		"Get-NetAdapter -CimSession server",
		"Get-DnsClientCache -CimSession server",
		"Get-WinEvent -FilterXml '<x />'",
		"Get-WinEvent -FilterHashtable @{LogName='System'}",
		"Get-WinEvent -ComputerName server -LogName System",
		"Get-WmiObject -Class Win32_Process",
		"Get-CimInstance -ClassName Win32_Process",
		"Get-CimClass -CimSession server -ClassName Win32_Process",
		"ipconfig set en0 DHCP",
		"ipconfig /flushdns",
		"hostname new-hostname",
		"hostname -F hostname.txt",
		"route add 10.0.0.0 mask 255.0.0.0 192.168.1.1 print",
		"netsh interface ipv4 show addresses",
		"arp -d 127.0.0.1",
		"file -C README.md",
		"findstr /OFF TODO README.md",
		"dotnet build",
		"dotnet --info $env:SECRET",
		"docker run alpine",
		"docker rm app",
		"docker exec app id",
		"docker logs --since $env:SECRET app",
		"docker inspect --format $env:SECRET app",
		"docker inspect --privileged app",
		"Write-Output (Remove-Item out.txt)",
		"Invoke-RestMethod https://example.com",
		"git commit -m test",
		"git diff --output=/tmp/diff.patch",
		"git ls-remote https://evil.example/repo.git",
		`scripts\git.exe status --short`,
	}
	for _, command := range notReadOnly {
		if IsReadOnlyCommand(command) {
			t.Fatalf("%q should not be read-only", command)
		}
	}

	destructive := []string{
		"Remove-Item build -Recurse",
		"rm build",
		"rd build",
		"del out.txt",
		"Set-Content out.txt hi",
		"sc out.txt hi",
		"Add-Content out.txt hi",
		"ac out.txt hi",
		"Clear-Content out.txt",
		"clc out.txt",
		"Clear-Item variable:x",
		"cli variable:x",
		"New-Item out.txt",
		"mkdir build",
		"ni out.txt",
		"Move-Item a b",
		"mv a b",
		"Copy-Item a b",
		"cp a b",
		"ci a b",
		"Rename-Item a b",
		"ren a b",
		"Set-Item variable:x 1",
		"si variable:x 1",
		"Export-Csv out.csv",
		"Start-Transcript out.txt",
		"Stop-Process -Id 1",
		"kill -Id 1",
		"spps -Id 1",
		"start calc.exe",
		"saps calc.exe",
		"Invoke-Expression $x",
		"Get-Location && Remove-Item out.txt",
		"git reset --hard",
		"git.exe clean -fd",
		"git.cmd push --force origin main",
	}
	for _, command := range destructive {
		if !IsDestructiveCommand(command) {
			t.Fatalf("%q should be destructive", command)
		}
	}
}

func TestPowerShellToolDynamicSafetyFlags(t *testing.T) {
	ps := NewPowerShellTool()
	if !ps.IsReadOnly(json.RawMessage(`{"command":"Get-Location"}`)) {
		t.Fatalf("Get-Location should be read-only")
	}
	if !ps.IsConcurrencySafe(json.RawMessage(`{"command":"Get-Location"}`)) {
		t.Fatalf("read-only PowerShell command should be concurrency safe")
	}
	if ps.IsReadOnly(json.RawMessage(`{"command":"Set-Content out.txt hi"}`)) {
		t.Fatalf("Set-Content should not be read-only")
	}
	if !ps.IsDestructive(json.RawMessage(`{"command":"Remove-Item out.txt"}`)) {
		t.Fatalf("Remove-Item should be destructive")
	}
}

func TestPowerShellMissingExecutableReturnsStructuredError(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	executor := powerShellExecutor(t)
	result, err := executor.Execute(tool.Context{
		Context:  context.Background(),
		Metadata: map[string]any{},
	}, contracts.ToolUse{
		ID:    "toolu_missing_powershell",
		Name:  "PowerShell",
		Input: json.RawMessage(`{"command":"Write-Output hello"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatalf("missing executable should mark the tool result as an error")
	}
	if result.StructuredContent["exit_code"] != -1 {
		t.Fatalf("exit code = %#v", result.StructuredContent["exit_code"])
	}
	stderr, _ := result.StructuredContent["stderr"].(string)
	if !strings.Contains(stderr, "PowerShell executable not found") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestPowerShellRunInBackgroundAndReadOutput(t *testing.T) {
	requirePowerShell(t)
	executor := powerShellExecutor(t)
	ctx := WithBackgroundState(tool.Context{
		Context:  context.Background(),
		Metadata: map[string]any{},
	}, NewBackgroundState())
	start, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_powershell_background",
		Name:  "PowerShell",
		Input: json.RawMessage(`{"command":"Write-Output 'one'; Write-Output 'two'; Write-Output 'three'","run_in_background":true,"description":"background print"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(start.Content.(string), "PowerShell command started in background with ID: powershell_") {
		t.Fatalf("start content = %#v", start.Content)
	}
	powerShellID := start.StructuredContent["powershell_id"].(string)
	if powerShellID == "" || start.StructuredContent["running"] != true {
		t.Fatalf("start structured content = %#v", start.StructuredContent)
	}

	output := waitForPowerShellOutput(t, executor, ctx, powerShellID)
	if output.IsError {
		t.Fatalf("output should not be error: %#v", output)
	}
	if output.StructuredContent["running"] != false || output.StructuredContent["exit_code"] != 0 {
		t.Fatalf("output structured content = %#v", output.StructuredContent)
	}
	if normalizeNewlines(output.StructuredContent["stdout"].(string)) != "one\ntwo\nthree\n" {
		t.Fatalf("stdout = %#v", output.StructuredContent["stdout"])
	}

	tail, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_powershell_background_tail",
		Name:  "PowerShellOutput",
		Input: json.RawMessage(`{"powershell_id":` + strconvQuote(powerShellID) + `,"tail_lines":2}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if normalizeNewlines(tail.StructuredContent["stdout"].(string)) != "two\nthree" {
		t.Fatalf("tail stdout = %#v", tail.StructuredContent["stdout"])
	}
}

func TestPowerShellBackgroundTimeout(t *testing.T) {
	requirePowerShell(t)
	executor := powerShellExecutor(t)
	ctx := WithBackgroundState(tool.Context{
		Context:  context.Background(),
		Metadata: map[string]any{},
	}, NewBackgroundState())
	start, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_powershell_background_timeout",
		Name:  "PowerShell",
		Input: json.RawMessage(`{"command":"Start-Sleep -Milliseconds 1000","runInBackground":true,"timeout":50}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	powerShellID := start.StructuredContent["powershell_id"].(string)
	output := waitForPowerShellOutput(t, executor, ctx, powerShellID)
	if !output.IsError {
		t.Fatalf("timeout output should be error: %#v", output)
	}
	if output.StructuredContent["timed_out"] != true || output.StructuredContent["exit_code"] != -1 {
		t.Fatalf("timeout structured content = %#v", output.StructuredContent)
	}
	if !strings.Contains(output.Content.(string), "timed out after 50ms") {
		t.Fatalf("timeout content = %#v", output.Content)
	}
}

func TestKillPowerShellCancelsBackgroundCommand(t *testing.T) {
	requirePowerShell(t)
	executor := powerShellExecutor(t)
	ctx := WithBackgroundState(tool.Context{
		Context:  context.Background(),
		Metadata: map[string]any{},
	}, NewBackgroundState())
	start, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_powershell_background_kill_start",
		Name:  "PowerShell",
		Input: json.RawMessage(`{"command":"Start-Sleep -Seconds 5","run_in_background":true,"timeout":5000}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	powerShellID := start.StructuredContent["powershell_id"].(string)

	killed, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_powershell_background_kill",
		Name:  "KillPowerShell",
		Input: json.RawMessage(`{"powershell_id":` + strconvQuote(powerShellID) + `}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if killed.StructuredContent["killed"] != true || killed.StructuredContent["cancelled"] != true {
		t.Fatalf("kill structured content = %#v", killed.StructuredContent)
	}

	output := waitForPowerShellOutput(t, executor, ctx, powerShellID)
	if !output.IsError {
		t.Fatalf("cancelled output should be error: %#v", output)
	}
	if output.StructuredContent["cancelled"] != true || output.StructuredContent["timed_out"] != false {
		t.Fatalf("cancelled structured content = %#v", output.StructuredContent)
	}
	if !strings.Contains(output.Content.(string), "was cancelled") {
		t.Fatalf("cancelled content = %#v", output.Content)
	}
}

func TestPowerShellOutputValidationAndMissingTask(t *testing.T) {
	executor := powerShellExecutor(t)
	ctx := WithBackgroundState(tool.Context{
		Context:  context.Background(),
		Metadata: map[string]any{},
	}, NewBackgroundState())
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "missing id", input: `{}`, want: "powershell_id is required"},
		{name: "bad tail", input: `{"powershell_id":"powershell_missing","tail_lines":0}`, want: "tail_lines must be positive"},
		{name: "unknown field", input: `{"powershell_id":"powershell_missing","extra":true}`, want: "input.extra is not allowed"},
		{name: "missing task", input: `{"id":"powershell_missing"}`, want: "background powershell command not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := executor.Execute(ctx, contracts.ToolUse{
				ID:    "toolu_powershell_output_invalid",
				Name:  "PowerShellOutput",
				Input: json.RawMessage(tt.input),
			}, nil)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestPowerShellOutputPersistsOversizedBackgroundOutput(t *testing.T) {
	executor := powerShellExecutor(t)
	executor.ResultStoreDir = t.TempDir()
	state := NewBackgroundState()
	task := &BackgroundTask{
		ID:          "powershell_large",
		Command:     "Write-Output large",
		Description: "large output",
		StartedAt:   time.Now().Add(-time.Second),
		EndedAt:     time.Now(),
		TimeoutMS:   defaultTimeoutMillis,
		Running:     false,
		ExitCode:    0,
	}
	_, _ = task.stdout.Write([]byte(strings.Repeat("x", 110_000)))
	state.Add(task)
	ctx := WithBackgroundState(tool.Context{
		Context:  context.Background(),
		Metadata: map[string]any{},
	}, state)

	result, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_powershell_large_output",
		Name:  "PowerShellOutput",
		Input: json.RawMessage(`{"powershell_id":"powershell_large"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Meta["truncated"] != true {
		t.Fatalf("result should be truncated: meta=%#v", result.Meta)
	}
	path, _ := result.Meta["full_output_path"].(string)
	if path == "" {
		t.Fatalf("full output path missing: meta=%#v", result.Meta)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), strings.Repeat("x", 110_000)) {
		t.Fatalf("persisted output does not contain full stdout")
	}
	if !strings.Contains(result.Content.(string), "Tool output truncated") {
		t.Fatalf("truncated content marker missing: %#v", result.Content)
	}
}

func TestPowerShellForegroundPersistsOversizedOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake pwsh executable test uses a POSIX shell script")
	}
	dir := t.TempDir()
	fakePwsh := filepath.Join(dir, "pwsh")
	script := "#!/bin/sh\n" +
		"i=0\n" +
		"while [ \"$i\" -lt 110000 ]; do printf x; i=$((i+1)); done\n"
	if err := os.WriteFile(fakePwsh, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	executor := powerShellExecutor(t)
	executor.ResultStoreDir = t.TempDir()
	result, err := executor.Execute(tool.Context{
		Context:  context.Background(),
		Metadata: map[string]any{},
	}, contracts.ToolUse{
		ID:    "toolu_powershell_large_foreground",
		Name:  "PowerShell",
		Input: json.RawMessage(`{"command":"Write-Output large"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Meta["truncated"] != true {
		t.Fatalf("result should be truncated: meta=%#v", result.Meta)
	}
	path, _ := result.Meta["full_output_path"].(string)
	if path == "" {
		t.Fatalf("full output path missing: meta=%#v", result.Meta)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 110000 {
		t.Fatalf("stored output length = %d", len(data))
	}
	if stdout := result.StructuredContent["stdout"].(string); len(stdout) != 110000 {
		t.Fatalf("structured stdout length = %d", len(stdout))
	}
	if !strings.Contains(result.Content.(string), "Tool output truncated") {
		t.Fatalf("truncated content marker missing: %#v", result.Content)
	}
}

func TestKillPowerShellValidationAndMissingTask(t *testing.T) {
	executor := powerShellExecutor(t)
	ctx := WithBackgroundState(tool.Context{
		Context:  context.Background(),
		Metadata: map[string]any{},
	}, NewBackgroundState())
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "missing id", input: `{}`, want: "powershell_id is required"},
		{name: "unknown field", input: `{"powershell_id":"powershell_missing","extra":true}`, want: "input.extra is not allowed"},
		{name: "missing task", input: `{"id":"powershell_missing"}`, want: "background powershell command not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := executor.Execute(ctx, contracts.ToolUse{
				ID:    "toolu_powershell_kill_invalid",
				Name:  "KillPowerShell",
				Input: json.RawMessage(tt.input),
			}, nil)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestPowerShellRunsWhenAvailable(t *testing.T) {
	requirePowerShell(t)
	executor := powerShellExecutor(t)
	result, err := executor.Execute(tool.Context{
		Context:  context.Background(),
		Metadata: map[string]any{},
	}, contracts.ToolUse{
		ID:    "toolu_powershell",
		Name:  "PowerShell",
		Input: json.RawMessage(`{"command":"Write-Output hello","description":"print greeting"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("result should not be an error: %#v", result)
	}
	if strings.TrimSpace(result.StructuredContent["stdout"].(string)) != "hello" {
		t.Fatalf("structured content = %#v", result.StructuredContent)
	}
	if result.StructuredContent["description"] != "print greeting" {
		t.Fatalf("description = %#v", result.StructuredContent["description"])
	}
}

func waitForPowerShellOutput(t *testing.T, executor tool.Executor, ctx tool.Context, powerShellID string) contracts.ToolResult {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		output, err := executor.Execute(ctx, contracts.ToolUse{
			ID:    "toolu_powershell_background_output",
			Name:  "PowerShellOutput",
			Input: json.RawMessage(`{"powershell_id":` + strconvQuote(powerShellID) + `}`),
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if output.StructuredContent["running"] == false {
			return output
		}
		if time.Now().After(deadline) {
			t.Fatalf("background PowerShell command %s did not finish; last output = %#v", powerShellID, output.StructuredContent)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func requirePowerShell(t *testing.T) {
	t.Helper()
	if _, ok := powerShellExecutable(); !ok {
		t.Skip("PowerShell executable is not installed")
	}
}

func normalizeNewlines(text string) string {
	return strings.ReplaceAll(text, "\r\n", "\n")
}

func strconvQuote(value string) string {
	escaped, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(escaped)
}

func TestPowerShellExecutableDetectionAllowsMissingBinary(t *testing.T) {
	_, ok := powerShellExecutable()
	if !ok {
		if _, err := exec.LookPath("pwsh"); err == nil {
			t.Fatalf("pwsh exists but detector did not find it")
		}
		if _, err := exec.LookPath("powershell"); err == nil {
			t.Fatalf("powershell exists but detector did not find it")
		}
	}
}
