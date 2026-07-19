[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$AdapterName,

    [string]$NetworkName = "transparent",
    [string]$Subnet,
    [string]$RouterIP,
    [string]$MetadataURL = "http://metadata/2016-07-29",
    [string]$MetadataCARoot,
    [string]$ExecutablePath = "$env:ProgramFiles\PastureStack\per-host-subnet.exe",
    [switch]$Apply
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$ServiceName = "pasturestack-per-host-subnet"
$SubnetLabel = "io.pasturestack.network.per-host-subnet.subnet"
$RouterIPLabel = "io.pasturestack.network.per-host-subnet.router-ip"

function ConvertTo-IPv4Number {
    param([Parameter(Mandatory = $true)][System.Net.IPAddress]$Address)

    $bytes = $Address.GetAddressBytes()
    if ($bytes.Length -ne 4) {
        throw "Only IPv4 addresses are supported."
    }
    return ([uint64]$bytes[0] -shl 24) -bor ([uint64]$bytes[1] -shl 16) -bor ([uint64]$bytes[2] -shl 8) -bor [uint64]$bytes[3]
}

function Test-IPv4CIDRContains {
    param(
        [Parameter(Mandatory = $true)][string]$CIDR,
        [Parameter(Mandatory = $true)][string]$Address
    )

    $parts = $CIDR.Split("/")
    if ($parts.Length -ne 2) {
        return $false
    }
    $networkAddress = $null
    $candidateAddress = $null
    $prefix = 0
    if (-not [System.Net.IPAddress]::TryParse($parts[0], [ref]$networkAddress) -or
        -not [System.Net.IPAddress]::TryParse($Address, [ref]$candidateAddress) -or
        -not [int]::TryParse($parts[1], [ref]$prefix) -or
        $prefix -lt 1 -or $prefix -gt 30 -or
        $networkAddress.AddressFamily -ne [System.Net.Sockets.AddressFamily]::InterNetwork -or
        $candidateAddress.AddressFamily -ne [System.Net.Sockets.AddressFamily]::InterNetwork) {
        return $false
    }
    $mask = ([uint64]0xFFFFFFFF -shl (32 - $prefix)) -band [uint64]0xFFFFFFFF
    return ((ConvertTo-IPv4Number $networkAddress) -band $mask) -eq ((ConvertTo-IPv4Number $candidateAddress) -band $mask)
}

function Get-MetadataLabel {
    param(
        [Parameter(Mandatory = $true)]$Labels,
        [Parameter(Mandatory = $true)][string]$Name
    )

    $property = $Labels.PSObject.Properties[$Name]
    if ($null -eq $property -or [string]::IsNullOrWhiteSpace([string]$property.Value)) {
        throw "Metadata label '$Name' is missing."
    }
    return [string]$property.Value
}

function Get-NetworkConfiguration {
    if (-not [string]::IsNullOrWhiteSpace($script:Subnet) -and -not [string]::IsNullOrWhiteSpace($script:RouterIP)) {
        return
    }
    $selfHostURL = $MetadataURL.TrimEnd("/") + "/self/host"
    $hostMetadata = Invoke-RestMethod -Method Get -Uri $selfHostURL -UseBasicParsing
    if ([string]::IsNullOrWhiteSpace($script:Subnet)) {
        $script:Subnet = Get-MetadataLabel -Labels $hostMetadata.labels -Name $SubnetLabel
    }
    if ([string]::IsNullOrWhiteSpace($script:RouterIP)) {
        $script:RouterIP = Get-MetadataLabel -Labels $hostMetadata.labels -Name $RouterIPLabel
    }
}

function Test-ExistingNetwork {
    $raw = & docker network inspect $NetworkName 2>$null
    if ($LASTEXITCODE -ne 0) {
        return $false
    }
    $network = $raw | ConvertFrom-Json
    $candidate = @($network)[0]
    if ($candidate.Driver -ne "transparent") {
        throw "Docker network '$NetworkName' exists with a different driver."
    }
    $ipam = @($candidate.IPAM.Config)[0]
    if ($ipam.Subnet -ne $Subnet -or $ipam.Gateway -ne $RouterIP) {
        throw "Docker network '$NetworkName' exists with different address settings."
    }
    $configuredAdapter = [string]$candidate.Options."com.docker.network.windowsshim.interface"
    if ($configuredAdapter -ne $AdapterName) {
        throw "Docker network '$NetworkName' exists on a different adapter."
    }
    return $true
}

function Set-ServiceEnvironment {
    $environment = @(
        "PLATFORM_METADATA_URL=$MetadataURL",
        "PLATFORM_ENABLE_ROUTE_UPDATE=true",
        "PLATFORM_NAT_INTERFACE=$AdapterName"
    )
    if (-not [string]::IsNullOrWhiteSpace($MetadataCARoot)) {
        $environment += "PLATFORM_CA_ROOT=$MetadataCARoot"
    }
    $serviceKey = "HKLM:\SYSTEM\CurrentControlSet\Services\$ServiceName"
    New-ItemProperty -Path $serviceKey -Name Environment -PropertyType MultiString -Value $environment -Force | Out-Null
}

Get-NetworkConfiguration
if (-not (Test-IPv4CIDRContains -CIDR $Subnet -Address $RouterIP)) {
    throw "RouterIP must be a usable IPv4 address inside Subnet."
}
if (-not (Test-Path -LiteralPath $ExecutablePath -PathType Leaf)) {
    throw "Executable not found: $ExecutablePath"
}
$adapter = Get-NetAdapter -Name $AdapterName
if ($adapter.Status -ne "Up") {
    throw "Adapter '$AdapterName' is not up."
}
$remoteAccess = Get-Service -Name RemoteAccess -ErrorAction SilentlyContinue
if ($null -eq $remoteAccess -or $remoteAccess.Status -ne "Running") {
    throw "The Windows Routing and Remote Access service must be configured and running before this tool is applied."
}

$networkExists = Test-ExistingNetwork
if (-not $Apply) {
    Write-Output "Validation succeeded. No changes were made."
    Write-Output "Network exists: $networkExists"
    Write-Output "Re-run with -Apply to create the network when absent and register the service."
    return
}

if (-not $networkExists) {
    & docker network create --driver transparent --subnet $Subnet --gateway $RouterIP --opt "com.docker.network.windowsshim.interface=$AdapterName" $NetworkName | Out-Null
    if ($LASTEXITCODE -ne 0) {
        throw "Docker network creation failed."
    }
}

$existingService = Get-CimInstance Win32_Service -Filter "Name='$ServiceName'" -ErrorAction SilentlyContinue
if ($null -ne $existingService) {
    $expectedPath = [System.IO.Path]::GetFullPath($ExecutablePath)
    $registeredPath = [string]$existingService.PathName
    $quotedExpectedPath = '"' + $expectedPath + '"'
    if ($registeredPath -ne $expectedPath -and
        -not $registeredPath.StartsWith($quotedExpectedPath + " ", [System.StringComparison]::OrdinalIgnoreCase)) {
        throw "The existing service points to a different executable and was not changed."
    }
    Stop-Service -Name $ServiceName -Force -ErrorAction SilentlyContinue
    & $ExecutablePath --unregister-service
    if ($LASTEXITCODE -ne 0) {
        throw "Service unregistration failed."
    }
}

& $ExecutablePath --register-service
if ($LASTEXITCODE -ne 0) {
    throw "Service registration failed."
}
Set-ServiceEnvironment
Start-Service -Name $ServiceName
Write-Output "PastureStack per-host subnet networking is configured."
