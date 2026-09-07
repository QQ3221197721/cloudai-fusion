param(
    [Parameter(Mandatory=$true)][string]$File,
    [Parameter(Mandatory=$true)][string]$Pattern,
    [Parameter(Mandatory=$true)][string]$Replacement
)
$enc = New-Object System.Text.UTF8Encoding($false)
$text = [System.IO.File]::ReadAllText($File)
$new = [System.Text.RegularExpressions.Regex]::Replace($text, $Pattern, $Replacement)
if ($new -ne $text) {
    [System.IO.File]::WriteAllText($File, $new, $enc)
    $count = ([System.Text.RegularExpressions.Regex]::Matches($text, $Pattern)).Count
    Write-Output "REPLACED $count occurrence(s) in $File"
} else {
    Write-Output "NOCHANGE: $File"
}
