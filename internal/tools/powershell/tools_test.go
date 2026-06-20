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
		{name: "standalone long sleep", input: `{"command":"Start-Sleep -Seconds 2"}`, want: "Blocked: standalone Start-Sleep 2"},
		{name: "leading long sleep alias", input: `{"command":"sleep 2; Get-Process"}`, want: "Blocked: Start-Sleep 2 followed by: Get-Process"},
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

func TestPowerShellAcceptsSemanticStringInputs(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	executor := powerShellExecutor(t)
	ctx := WithBackgroundState(tool.Context{
		Context:  context.Background(),
		Metadata: map[string]any{},
	}, NewBackgroundState())
	result, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_powershell_semantic_timeout",
		Name:  "PowerShell",
		Input: json.RawMessage(`{"command":"Write-Output semantic","timeout":"1000","dangerouslyDisableSandbox":"true"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || result.StructuredContent["timeout_ms"] != 1000 || result.StructuredContent["dangerously_disable_sandbox"] != true {
		t.Fatalf("semantic timeout result = %#v", result)
	}
	stderr, _ := result.StructuredContent["stderr"].(string)
	if !strings.Contains(stderr, "PowerShell executable not found") {
		t.Fatalf("stderr = %q", stderr)
	}

	start, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_powershell_semantic_background",
		Name:  "PowerShell",
		Input: json.RawMessage(`{"command":"Write-Output semantic","runInBackground":"true","timeout":"1000"}`),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !start.IsError || start.StructuredContent["timeout_ms"] != 1000 {
		t.Fatalf("semantic background result = %#v", start)
	}

	_, err = executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_powershell_semantic_tail",
		Name:  "PowerShellOutput",
		Input: json.RawMessage(`{"id":"powershell_missing","tailLines":"2"}`),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "background powershell command not found") {
		t.Fatalf("err = %v, want missing background task", err)
	}
}

func TestPowerShellCommandClassification(t *testing.T) {
	readOnly := []string{
		"Get-Location",
		"pwd",
		"Get-ChildItem -Force",
		"Get-ChildItem -Filter '*.go' ./internal",
		"Get-ChildItem -Depth 2 ./internal",
		"Get-ChildItem -Include '*.go' ./internal",
		"Get-ChildItem -Attributes Directory ./internal",
		"Get-ChildItem ./internal",
		"Get-Content README.md",
		"Get-Content -Raw README.md",
		"Get-Content -Encoding UTF8 README.md",
		"Get-Content -ReadCount 0 README.md",
		"Get-Content -Tail 10 README.md",
		"Get-Content -First 5 README.md",
		"Get-Content -EA Stop README.md",
		`Get-Content .\README.md`,
		"type README.md",
		"gi README.md",
		"Get-Item -Stream Zone.Identifier README.md",
		"gp . -Name Mode",
		"Select-String TODO README.md",
		"Select-String -Pattern TODO -Path README.md",
		"Select-String -SimpleMatch TODO README.md",
		"sls TODO README.md",
		"ConvertTo-Json -InputObject hello -Depth 2",
		`ConvertFrom-Json '{"a":1}'`,
		"ConvertTo-Csv -InputObject value -NoTypeInformation",
		"ConvertFrom-Csv -Header Name one",
		"ConvertTo-Xml -InputObject value -Depth 1",
		"ConvertTo-Html -Title report -Fragment",
		"Get-Member -Name Length",
		"Get-Unique -AsString value",
		"Compare-Object -ReferenceObject a -DifferenceObject b",
		"compare -ReferenceObject a -DifferenceObject b",
		"diff a b",
		"Join-String -Separator , a b",
		"Get-Random -Minimum 1 -Maximum 10",
		"Convert-Path README.md",
		"Resolve-Path README.md",
		"rvpa README.md",
		"rvpa -Relative ./internal",
		"Test-Path README.md",
		"Test-Path -Path README.md -PathType Leaf",
		"Test-Path -Path ./internal -Filter '*.go'",
		"Split-Path README.md -Leaf",
		"Split-Path -Path internal/tools/powershell/tools.go -Parent",
		"Split-Path -LiteralPath README.md -IsAbsolute",
		"Join-Path internal tools",
		"Join-Path -Path internal -ChildPath tools",
		"Join-Path -LiteralPath internal -ChildPath tools -AdditionalChildPath powershell",
		"Get-ItemProperty . -Name Mode",
		"Get-ItemPropertyValue . -Name Mode",
		"gip . -Name Mode",
		"gpv . -Name Mode",
		"Get-Acl README.md",
		"Get-Acl -Path README.md -Filter '*.md'",
		"Get-Acl -LiteralPath README.md -Audit",
		"Get-FileHash README.md",
		"Get-FileHash -Algorithm SHA256 README.md",
		"Get-FileHash -Path README.md -Algorithm SHA512",
		"Format-Hex README.md",
		"Format-Hex -Path README.md -Count 16",
		"Format-Hex -InputObject hello -Encoding UTF8",
		"Get-PSProvider -PSProvider FileSystem",
		"Get-Process | Select-String go",
		"Get-Process | Select-Object Name",
		"Get-Process | Select-Object -First 5 Name",
		"Get-Process | Sort-Object Name",
		"Get-Process | Group-Object Name",
		"Get-Process | Where-Object Name -EQ go",
		"Get-Process | select Name",
		"Get-Process | sort Name",
		"Get-Process | group Name",
		"Get-Process | where Name -EQ go",
		"Get-Process | ft Name",
		"Get-Process | fl Name",
		"Get-Process | fw Name",
		"Get-Process | fc Name",
		"Get-Process | measure CPU",
		"Get-Process | Format-Table -AutoSize Name",
		"Get-Process | Format-List Name",
		"Get-Process | Format-Wide Name",
		"Get-Process | Format-Custom Name",
		"Get-Process | Measure-Object -Property CPU",
		"Get-Process | Out-String -Width 120",
		"Get-Process | Out-Host",
		"Get-Process -Name go",
		"Get-Process -WA SilentlyContinue",
		"Get-Process -Infa Continue",
		"Get-Process -Proga SilentlyContinue",
		"Get-Process -IV info",
		"Get-Process -VB",
		"Get-Process -DB",
		"ps -Name go",
		"Get-Service -Name ssh-agent",
		"Get-Service -DisplayName ssh-agent",
		"gsv -Name ssh-agent",
		"Get-Date -Format o",
		"Get-PSDrive -Name C",
		"Get-Module -ListAvailable",
		"Get-Alias -Name ls",
		"gal -Name ls",
		"Get-History -Count 5",
		"h -Count 5",
		"ghy -Count 5",
		"gdr -Name C",
		"gmo -ListAvailable",
		"gm -Name Length",
		"gu -AsString value",
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
		"where.exe /R . git",
		"where.exe /Q /F /T git",
		"hostname -s",
		"whoami /all /fo csv",
		"ver",
		"arp -a",
		"route print",
		"route -4 print",
		"getmac /V /FO CSV",
		"file --mime-type README.md",
		"file -p README.md",
		"tree /F /A .",
		"findstr /I TODO README.md",
		"findstr /C:TODO README.md",
		"findstr '/D:src;logs' TODO README.md",
		"sha256sum README.md",
		"sha512sum.exe --binary archive.tar",
		"md5sum -b README.md",
		"b2sum --length 256 README.md",
		"shasum -a 256 README.md",
		"cksum --algorithm crc README.md",
		"sum -r README.md",
		"certutil -hashfile README.md SHA256",
		"certutil.exe -hashfile README.md",
		`certutil -hashfile .\README.md SHA1`,
		"fc.exe /n README.md docs/cc-100-roadmap.md",
		"comp.exe old.bin new.bin /d",
		"sort.exe README.md",
		"sort.exe /R README.md",
		"more.com README.md",
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
		"Get-Location # Remove-Item out.txt",
		"Write-Output '# Remove-Item out.txt'",
		"Write-Output '(Remove-Item out.txt)'",
		"Get-Location\nGet-Process",
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
		"Get-ChildItem -Filter $env:SECRET ./internal",
		"Get-ChildItem -Attributes @{Name='Directory'} ./internal",
		"Get-Content '$HOME/file'",
		"Select-String TODO /etc/passwd",
		"sls TODO /etc/passwd",
		"Select-String $env:SECRET README.md",
		"Select-String -Pattern $env:SECRET -Path README.md",
		"Select-String -InputObject $env:SECRET -Pattern TODO",
		"Select-String -Pattern TODO -Context @{Before=1} README.md",
		"Get-Content --% README.md",
		"Get-Content -Wait README.md",
		"Get-Content -Tail 10 -Wait README.md",
		"Get-Content 'README.md",
		"Get-Content \"README.md",
		"Write-Output hello `",
		"Get-Content 'README.md; Remove-Item out.txt",
		"Get-Location\nInvoke-RestMethod https://example.com",
		"Get-Content -Credential admin README.md",
		"Get-Content -Raw:unexpected README.md",
		"Get-Content -Encoding",
		"Get-Content -First",
		"Get-Content -First 5 /etc/passwd",
		"Get-Content -OV x README.md",
		"Get-Content -Encoding $env:ENC README.md",
		"Get-Content -ReadCount @{N=1} README.md",
		"Get-Item -Stream $env:STREAM README.md",
		"Get-Process -PV p",
		"Get-Process -Infa",
		"Get-Process -Proga",
		"Get-ChildItem -Credential admin ./internal",
		"Select-String -Pattern TODO -Path /etc/passwd",
		"Get-Process -ComputerName server",
		"Write-Output -OutFile out.txt hello",
		"Get-Command -Name git",
		"Get-Help Get-Content",
		"Get-Clipboard",
		"gal -OV aliases",
		"gdr -OV drives",
		"gmo -CimSession server",
		`Select-Xml -Content '<x />' -XPath /`,
		`Test-Json '{}'`,
		"ConvertTo-Json -OutFile out.json hello",
		"Get-Module -CimSession server",
		"Get-Date -ComputerName server",
		"Get-ComputerInfo -CimSession server",
		"Get-Host -AsJob",
		"Get-Culture -Credential admin",
		"Start-Sleep -Seconds $env:SECRET",
		`Get-ItemProperty HKLM:\Software -Name Path`,
		`gip HKLM:\Software -Name Path`,
		`gpv C:\Users\secret.txt -Name Mode`,
		"Get-ItemProperty . -Name $env:SECRET",
		"Get-ItemPropertyValue . -Name @{N='Mode'}",
		`rvpa C:\Users\secret.txt`,
		"rvpa ../secret.txt",
		"Test-Path /etc/passwd",
		"Test-Path -Path $env:SECRET",
		"Test-Path -Path README.md -Filter $env:SECRET",
		"Test-Path -Path README.md -PathType @{Name='Leaf'}",
		"Split-Path /etc/passwd -Leaf",
		`Split-Path C:\Users\secret.txt -Leaf`,
		"Split-Path -Path $env:SECRET -Leaf",
		"Split-Path -Resolve README.md",
		"Join-Path /etc passwd",
		`Join-Path C:\Users secret.txt`,
		"Join-Path -Path internal -ChildPath $env:SECRET",
		"Join-Path -Resolve internal tools",
		"Get-Acl /etc/passwd",
		`Get-Acl -Path C:\Users\secret.txt`,
		"Get-Acl -Path README.md -Filter $env:SECRET",
		"Get-FileHash /etc/passwd",
		`Get-FileHash -Path C:\Users\secret.txt`,
		"Get-FileHash -Algorithm $env:ALG README.md",
		"Get-FileHash -InputStream $stream",
		`Get-FileHash -InputStream ([IO.File]::OpenRead('/etc/passwd'))`,
		"Format-Hex /etc/passwd",
		`Format-Hex -Path C:\Users\secret.txt`,
		"Format-Hex -InputObject $env:SECRET",
		"Get-Process | Select-Object $env:SECRET",
		"Get-Process | select $env:SECRET",
		"Get-Process | Select-Object @{N='x';E={Remove-Item out.txt}}",
		"Get-Process | Where-Object { Remove-Item out.txt }",
		"Get-Process | Format-Table -Property:$env:SECRET",
		"Get-Process | Out-String -InputObject:$env:SECRET",
		"Get-Process | Out-Host -Paging",
		"Get-Process -Name $env:SECRET",
		"Get-Process -Id @{N=1}",
		"Get-Service -Name $env:SECRET",
		"Get-Service -DisplayName @{N='ssh-agent'}",
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
		`where.exe /R C:\Users git`,
		`where.exe /R \\server\share git`,
		"where.exe /R .. git",
		"where.exe /R file://repo git",
		"where.exe /R",
		"where.exe /R /Q git",
		"where.exe /Z git",
		"file -C README.md",
		"file -f files.txt",
		"file -ffiles.txt",
		"file --files-from=files.txt",
		`file -p C:\Users\secret.txt`,
		`file C:\Users\secret.txt`,
		`file -f C:\Users\files.txt`,
		`tree \\server\share`,
		"findstr /OFF TODO README.md",
		"findstr TODO file://README.md",
		`findstr TODO C:\Users\secret.txt`,
		`findstr /C:TODO C:\Users\secret.txt`,
		`findstr /G:C:\Users\patterns.txt README.md`,
		`findstr /D:..\secret TODO README.md`,
		`findstr '/D:src;..\secret' TODO README.md`,
		`findstr '/D:src;\\server\share' TODO README.md`,
		"findstr /G",
		"sha256sum /etc/passwd",
		`sha256sum C:\Users\secret.txt`,
		"sha256sum -c checksums.txt",
		"sha256sum --check checksums.txt",
		"md5sum --check=checksums.txt",
		"shasum -c checksums.txt",
		"certutil -hashfile /etc/passwd SHA256",
		`certutil -hashfile C:\Users\secret.txt SHA256`,
		"certutil -hashfile README.md $env:ALG",
		"certutil -hashfile README.md SHA256 extra",
		"certutil --% -hashfile README.md SHA256",
		"certutil -urlcache -split -f https://example.com/file out.bin",
		"fc.exe",
		"fc.exe README.md",
		"fc.exe README.md /etc/passwd",
		"comp old.bin new.bin /n=10",
		`comp.exe old.bin C:\Users\secret.bin`,
		"comp.exe old.bin new.bin /n $env:COUNT",
		"sort.exe",
		"sort.exe /etc/passwd",
		"sort.exe README.md /O out.txt",
		"sort.exe README.md /O:out.txt",
		"sort.exe --% README.md",
		"more.com",
		"more README.md",
		"more.com /etc/passwd",
		"more.com --% README.md",
		"diff.exe README.md /etc/passwd",
		"dotnet build",
		"dotnet --info $env:SECRET",
		"docker run alpine",
		"docker rm app",
		"docker exec app id",
		"docker logs --follow app",
		"docker logs -f app",
		"docker logs --since $env:SECRET app",
		"docker inspect --format $env:SECRET app",
		"docker inspect --privileged app",
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
		"Get-Location\nRemove-Item out.txt",
		"Get-Location # comment\nRemove-Item out.txt",
		"Write-Output (Remove-Item out.txt)",
		`Write-Output "$(Remove-Item out.txt)"`,
		"& { Remove-Item out.txt }",
		"Get-Process | Select-Object @{N='x';E={Remove-Item out.txt}}",
		"git reset --hard",
		"git.exe clean -fd",
		"git.cmd push --force origin main",
	}
	for _, command := range destructive {
		if !IsDestructiveCommand(command) {
			t.Fatalf("%q should be destructive", command)
		}
	}
	if IsDestructiveCommand("sc.exe query") {
		t.Fatalf("native sc.exe should not be classified as Set-Content alias")
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
	if tail.StructuredContent["tail_lines"] != 2 {
		t.Fatalf("tail structured content = %#v", tail.StructuredContent)
	}
}

func TestPowerShellBackgroundProgressEvents(t *testing.T) {
	requirePowerShell(t)
	executor := powerShellExecutor(t)
	ctx := WithBackgroundState(tool.Context{
		Context:  context.Background(),
		Metadata: map[string]any{},
	}, NewBackgroundState())
	progressCh := make(chan contracts.ToolProgress, 8)
	result, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_powershell_background_progress",
		Name:  "PowerShell",
		Input: json.RawMessage(`{"command":"Write-Output progress","run_in_background":true}`),
	}, tool.ProgressFunc(func(progress contracts.ToolProgress) error {
		progressCh <- progress
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	powerShellID := result.StructuredContent["powershell_id"].(string)
	started := waitForPowerShellProgress(t, progressCh, "powershell_background_started")
	if started.ToolUseID != "toolu_powershell_background_progress" || started.Data["powershell_id"] != powerShellID || started.Data["status"] != "running" {
		t.Fatalf("started progress = %#v", started)
	}
	if _, ok := started.Data["command"]; ok {
		t.Fatalf("started progress should not expose command: %#v", started.Data)
	}
	finished := waitForPowerShellProgress(t, progressCh, "powershell_background_finished")
	if finished.ToolUseID != "toolu_powershell_background_progress" || finished.Data["powershell_id"] != powerShellID || finished.Data["status"] != "completed" {
		t.Fatalf("finished progress = %#v", finished)
	}
	if finished.Data["exit_code"] != 0 || finished.Data["timed_out"] != false || finished.Data["cancelled"] != false {
		t.Fatalf("finished progress status = %#v", finished.Data)
	}
	if stdoutBytes, ok := finished.Data["stdout_bytes"].(int); !ok || stdoutBytes <= 0 {
		t.Fatalf("finished stdout bytes = %#v", finished.Data)
	}
	if _, ok := finished.Data["command"]; ok {
		t.Fatalf("finished progress should not expose command: %#v", finished.Data)
	}
}

func TestPowerShellBackgroundTimeoutProgressEvent(t *testing.T) {
	requirePowerShell(t)
	executor := powerShellExecutor(t)
	ctx := WithBackgroundState(tool.Context{
		Context:  context.Background(),
		Metadata: map[string]any{},
	}, NewBackgroundState())
	progressCh := make(chan contracts.ToolProgress, 8)
	result, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_powershell_background_timeout_progress",
		Name:  "PowerShell",
		Input: json.RawMessage(`{"command":"Start-Sleep -Milliseconds 1000","run_in_background":true,"timeout":50}`),
	}, tool.ProgressFunc(func(progress contracts.ToolProgress) error {
		progressCh <- progress
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	powerShellID := result.StructuredContent["powershell_id"].(string)
	finished := waitForPowerShellProgress(t, progressCh, "powershell_background_finished")
	if finished.ToolUseID != "toolu_powershell_background_timeout_progress" || finished.Data["powershell_id"] != powerShellID || finished.Data["status"] != "timed_out" {
		t.Fatalf("finished progress = %#v", finished)
	}
	if finished.Data["exit_code"] != -1 || finished.Data["timed_out"] != true || finished.Data["cancelled"] != false {
		t.Fatalf("timeout progress status = %#v", finished.Data)
	}
	if _, ok := finished.Data["command"]; ok {
		t.Fatalf("timeout progress should not expose command: %#v", finished.Data)
	}
}

func TestPowerShellBackgroundCancelProgressEvent(t *testing.T) {
	requirePowerShell(t)
	executor := powerShellExecutor(t)
	ctx := WithBackgroundState(tool.Context{
		Context:  context.Background(),
		Metadata: map[string]any{},
	}, NewBackgroundState())
	progressCh := make(chan contracts.ToolProgress, 8)
	result, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_powershell_background_cancel_progress",
		Name:  "PowerShell",
		Input: json.RawMessage(`{"command":"Start-Sleep -Seconds 5","run_in_background":true,"timeout":5000}`),
	}, tool.ProgressFunc(func(progress contracts.ToolProgress) error {
		progressCh <- progress
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	powerShellID := result.StructuredContent["powershell_id"].(string)
	started := waitForPowerShellProgress(t, progressCh, "powershell_background_started")
	if started.Data["powershell_id"] != powerShellID {
		t.Fatalf("started progress = %#v", started)
	}
	if _, err := executor.Execute(ctx, contracts.ToolUse{
		ID:    "toolu_powershell_background_cancel_progress_kill",
		Name:  "KillPowerShell",
		Input: json.RawMessage(`{"powershell_id":` + strconvQuote(powerShellID) + `}`),
	}, nil); err != nil {
		t.Fatal(err)
	}
	finished := waitForPowerShellProgress(t, progressCh, "powershell_background_finished")
	if finished.ToolUseID != "toolu_powershell_background_cancel_progress" || finished.Data["powershell_id"] != powerShellID || finished.Data["status"] != "cancelled" {
		t.Fatalf("finished progress = %#v", finished)
	}
	if finished.Data["exit_code"] != -1 || finished.Data["timed_out"] != false || finished.Data["cancelled"] != true {
		t.Fatalf("cancel progress status = %#v", finished.Data)
	}
	if _, ok := finished.Data["command"]; ok {
		t.Fatalf("cancel progress should not expose command: %#v", finished.Data)
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

func TestPowerShellForegroundCancellation(t *testing.T) {
	requirePowerShell(t)
	executor := powerShellExecutor(t)
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct {
		result contracts.ToolResult
		err    error
	}, 1)
	go func() {
		result, err := executor.Execute(tool.Context{
			Context:  runCtx,
			Metadata: map[string]any{},
		}, contracts.ToolUse{
			ID:    "toolu_powershell_cancel",
			Name:  "PowerShell",
			Input: json.RawMessage(`{"command":"Write-Output started; while ($true) { Start-Sleep -Milliseconds 100 }","timeout":5000}`),
		}, nil)
		done <- struct {
			result contracts.ToolResult
			err    error
		}{result: result, err: err}
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatal(got.err)
		}
		if !got.result.IsError {
			t.Fatalf("cancelled foreground command should be error: %#v", got.result)
		}
		if got.result.StructuredContent["cancelled"] != true || got.result.StructuredContent["timed_out"] != false || got.result.StructuredContent["exit_code"] != -1 {
			t.Fatalf("cancelled structured content = %#v", got.result.StructuredContent)
		}
		if !strings.Contains(got.result.Content.(string), "Command cancelled.") {
			t.Fatalf("cancelled content = %#v", got.result.Content)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("foreground PowerShell command did not stop after context cancellation")
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
	deadline := time.Now().Add(60 * time.Second)
	if testDeadline, ok := t.Deadline(); ok {
		testDeadline = testDeadline.Add(-time.Second)
		if testDeadline.Before(deadline) {
			deadline = testDeadline
		}
	}
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
		time.Sleep(50 * time.Millisecond)
	}
}

func waitForPowerShellProgress(t *testing.T, progressCh <-chan contracts.ToolProgress, progressType string) contracts.ToolProgress {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case progress := <-progressCh:
			if progress.Type == progressType {
				return progress
			}
		case <-deadline:
			t.Fatalf("progress %s not observed", progressType)
		}
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
