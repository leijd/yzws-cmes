# e2e simulated frontend: sms-login -> trial-enter -> next-no -> create -> list verify -> dup/disable/binding
$ErrorActionPreference = "Continue"
$base = "http://127.0.0.1:18370/api/v1"
$phone = "13079123109"
$fail = 0
function Step($name, $ok, $detail) {
    if ($ok) { Write-Output "[PASS] $name $detail" }
    else { Write-Output "[FAIL] $name $detail"; $script:fail++ }
}

# 1. sms login
$send = Invoke-RestMethod -Method Post "$base/auth/trial/send-code" -ContentType "application/json" -Body (@{phone=$phone} | ConvertTo-Json)
Step "send-code" ($send.demo_code -match '^\d{6}$') "demo_code=$($send.demo_code)"
$verify = Invoke-RestMethod -Method Post "$base/auth/trial/verify" -ContentType "application/json" -Body (@{phone=$phone; code=$send.demo_code} | ConvertTo-Json)
Step "verify" ($verify.tenants.Count -ge 1) "tenants=$($verify.tenants.Count)"

# 2. trial enter
$enter = Invoke-RestMethod -Method Post "$base/public/trial-enter" -ContentType "application/json" -Body (@{phone=$phone; tenant_code=$verify.tenant.code; entry_token=$verify.tenant.entry_token} | ConvertTo-Json)
$token = $enter.access_token
Step "trial-enter" ($token.Length -gt 100) "tenant=$($enter.tenant.name)"
$h = @{Authorization = "Bearer $token"}

# 3. next employee no
$nextno = Invoke-RestMethod "$base/employees/next-no" -Headers $h
$no = $nextno.employee_no
if (-not $no) { $no = $nextno.next_no }
Step "next-no" ($no -match '-') "no=$no"

# 4. create person (manager, with phone -> blind index + encryption path)
$stamp = Get-Date -Format "HHmmss"
$empBody = @{person_type="manager"; name="e2eA$stamp"; password="Test12345678!"; role_ids=@(); phone="139" + $stamp + "1111" } | ConvertTo-Json
$create = Invoke-RestMethod -Method Post "$base/people" -Headers $h -ContentType "application/json" -Body $empBody
Step "create-person" ($create.employee_no -match '-') "no=$($create.employee_no) id=$($create.employee_id)"

# 5. list verify
$list = Invoke-RestMethod "$base/people?limit=50" -Headers $h
$found = $list.items | Where-Object { $_.id -eq $create.employee_id }
Step "list-contains-new" ($null -ne $found) "total=$($list.items.Count) name=$($found.name)"

# 6. duplicate phone rejected
$dupBody = @{person_type="worker"; name="e2eB$stamp"; password="Test12345678!"; role_ids=@(); phone="139" + $stamp + "1111"} | ConvertTo-Json
try {
    Invoke-RestMethod -Method Post "$base/people" -Headers $h -ContentType "application/json" -Body $dupBody | Out-Null
    Step "dup-phone-rejected" $false "duplicate phone NOT rejected"
} catch {
    Step "dup-phone-rejected" ($_.ErrorDetails.Message -match "DUPLICATE_PHONE") ""
}

# 7. disable (saga person.disable)
$disable = Invoke-RestMethod -Method Post "$base/people/$($create.employee_id)/disable" -Headers $h -ContentType "application/json" -Body "{}"
Step "disable-person" ($disable.disabled -eq $true) ""

# 8. disabled employee org-binding must be rejected
try {
    Invoke-RestMethod -Method Put "$base/people/$($create.employee_id)/org-binding" -Headers $h -ContentType "application/json" -Body (@{workshop_org_id="x"; line_org_id="y"; version=1} | ConvertTo-Json) | Out-Null
    Step "org-binding-disabled-rejected" $false "NOT rejected"
} catch {
    $m = $_.ErrorDetails.Message
    Step "org-binding-disabled-rejected" ($m -match "EMPLOYEE_NOT_ACTIVE|ORG_INVALID|VERSION_CONFLICT") $m
}

Write-Output "==== FAILURES: $fail ===="
