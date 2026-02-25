# LLM Proxy Quota

Quota management with Redis backend and plan/addon credit tracking.

### CheckQuota

```mermaid
flowchart TB
    Start(["CheckQuota(ctx, client, userID, estimatedCost)"]) --> HGetPlan

    subgraph SGGetPlan [" "]
        HGetPlan["**client.Get(ctx, quotaKey(userID, 'plan_dollars')).Result() → planCents, err**"]
        HGetPlan -->|redis.Nil| E1["return &CheckResult{HasQuota: false}"]
        HGetPlan -->|error| E2["return nil, err"]
        HGetPlan --> HParsePlan
    end

    subgraph SGParsePlan [" "]
        HParsePlan["**planCentsInt, _ := strconv.ParseInt(planCents, 10, 64)**"]
        HParsePlan --> HConvPlan["**planDollarsFloat := centsToDollars(planCentsInt)**"]
        HConvPlan --> HGetAddon
    end

    subgraph SGGetAddon [" "]
        HGetAddon["**client.Get(ctx, quotaKey(userID, 'addon_dollars')).Result() → addonCents, err**"]
        HGetAddon --> HParseAddon["**addonCentsInt, _ := strconv.ParseInt(addonCents, 10, 64)**"]
        HParseAddon --> HConvAddon["**addonDollarsFloat := centsToDollars(addonCentsInt)**"]
        HConvAddon --> HGetPercent
    end

    subgraph SGGetPercent [" "]
        HGetPercent["**client.Get(ctx, quotaKey(userID, 'plan_percent')).Result() → planPercent, err**"]
        HGetPercent --> HParsePercent["**planPercentInt, _ := strconv.Atoi(planPercent)**"]
        HParsePercent --> HCheckPlan
    end

    subgraph SGCheckPlan [" "]
        HCheckPlan["**planDollarsFloat >= estimatedCost**"]
        HCheckPlan -->|true| ReturnPlan(["return &CheckResult{HasQuota: true, Source: 'plan'}"])
        HCheckPlan -->|false| HCheckAddon
    end

    subgraph SGCheckAddon [" "]
        HCheckAddon["**addonDollarsFloat >= estimatedCost**"]
        HCheckAddon -->|true| ReturnAddon(["return &CheckResult{HasQuota: true, Source: 'addon', UsingAddon: true}"])
        HCheckAddon -->|false| ReturnNone(["return &CheckResult{HasQuota: false}"])
    end

    GetPlan["client.Get(ctx, quotaKey(userID, 'plan_dollars')).Result() → planCents, err"]
    GetAddon["client.Get(ctx, quotaKey(userID, 'addon_dollars')).Result() → addonCents, err"]
    GetPercent["client.Get(ctx, quotaKey(userID, 'plan_percent')).Result() → planPercent, err"]
    ParseInt["strconv.ParseInt(str, 10, 64) → int64, err"]
    Atoi["strconv.Atoi(str) → int, err"]
    CentsToDollars["centsToDollars(cents) → dollars"]

    click GetPlan href "https://pkg.go.dev/github.com/redis/go-redis/v9#Client.Get"
    click GetAddon href "https://pkg.go.dev/github.com/redis/go-redis/v9#Client.Get"
    click GetPercent href "https://pkg.go.dev/github.com/redis/go-redis/v9#Client.Get"
    click ParseInt href "https://pkg.go.dev/strconv#ParseInt"
    click Atoi href "https://pkg.go.dev/strconv#Atoi"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,ReturnPlan,ReturnAddon,ReturnNone mainFn
    class GetPlan,GetAddon,GetPercent,ParseInt,Atoi,CentsToDollars localPkgs
    class E1,E2 errorFlow
```

### ReserveQuota

```mermaid
flowchart TB
    Start(["ReserveQuota(ctx, client, userID, estimatedCost, source)"]) --> HPipeline

    subgraph SGPipeline [" "]
        HPipeline["**pipe := client.Pipeline()**"]
        HPipeline --> HDecr
    end

    subgraph SGDecr [" "]
        HDecr["**pipe.DecrBy(ctx, balanceKey, formatCents(estimatedCost))**"]
        HDecr --> HIncr
    end

    subgraph SGIncr [" "]
        HIncr["**pipe.IncrBy(ctx, reserveKey, formatCents(estimatedCost))**"]
        HIncr --> HExec
    end

    subgraph SGExec [" "]
        HExec["**pipe.Exec(ctx) → _, err**"]
        HExec -->|error| E1["return err"]
        HExec --> Return(["return nil"])
    end

    Pipeline["client.Pipeline()"]
    DecrBy["pipe.DecrBy(ctx, key, amount)"]
    IncrBy["pipe.IncrBy(ctx, key, amount)"]
    Exec["pipe.Exec(ctx)"]
    FormatCents["formatCents(dollars) → cents"]

    click Pipeline href "https://pkg.go.dev/github.com/redis/go-redis/v9#Client.Pipeline"
    click DecrBy href "https://pkg.go.dev/github.com/redis/go-redis/v9#Pipeline.DecrBy"
    click IncrBy href "https://pkg.go.dev/github.com/redis/go-redis/v9#Pipeline.IncrBy"
    click Exec href "https://pkg.go.dev/github.com/redis/go-redis/v9#Pipeline.Exec"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Return mainFn
    class Pipeline,DecrBy,IncrBy,Exec,FormatCents localPkgs
    class E1 errorFlow
```

### DeductQuota

```mermaid
flowchart TB
    Start(["DeductQuota(ctx, client, userID, reserved, actual, source)"]) --> HCalcDiff

    subgraph SGCalcDiff [" "]
        HCalcDiff["**diff := reserved - actual**"]
        HCalcDiff --> HCheckDiff
    end

    subgraph SGCheckDiff [" "]
        HCheckDiff["**diff > 0**"]
        HCheckDiff -->|true| HRefund
        HCheckDiff -->|false| HCheckNeg
    end

    subgraph SGRefund [" "]
        HRefund["**client.IncrBy(ctx, balanceKey, formatCents(diff)).Err() → err**"]
        HRefund -->|error| E1["return err"]
        HRefund --> HRelease
    end

    subgraph SGCheckNeg [" "]
        HCheckNeg["**diff < 0**"]
        HCheckNeg -->|true| HCharge
        HCheckNeg -->|false| HRelease
    end

    subgraph SGCharge [" "]
        HCharge["**additionalCost := -diff**"]
        HCharge --> HDecr["**client.DecrBy(ctx, balanceKey, formatCents(additionalCost)).Err() → err**"]
        HDecr -->|error| E2["return err"]
        HDecr --> HRelease
    end

    subgraph SGRelease [" "]
        HRelease["**client.DecrBy(ctx, reserveKey, formatCents(reserved)).Err() → err**"]
        HRelease -->|error| E3["return err"]
        HRelease --> Return(["return nil"])
    end

    IncrBy["client.IncrBy(ctx, key, amount).Err() → err"]
    DecrBy["client.DecrBy(ctx, key, amount).Err() → err"]

    click IncrBy href "https://pkg.go.dev/github.com/redis/go-redis/v9#Client.IncrBy"
    click DecrBy href "https://pkg.go.dev/github.com/redis/go-redis/v9#Client.DecrBy"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Return mainFn
    class IncrBy,DecrBy localPkgs
    class E1,E2,E3 errorFlow
```

### GetUserQuotaInfo

```mermaid
flowchart TB
    Start(["GetUserQuotaInfo(ctx, client, userID)"]) --> HPipeline

    subgraph SGPipeline [" "]
        HPipeline["**pipe := client.Pipeline()**"]
        HPipeline --> HGetAll
    end

    subgraph SGGetAll [" "]
        HGetAll["**Get all quota fields in parallel**"]
        HGetAll --> HExec
    end

    subgraph SGExec [" "]
        HExec["**pipe.Exec(ctx) → _, err**"]
        HExec -->|error && !redis.Nil| E1["return nil, err"]
        HExec --> HParseAll
    end

    subgraph SGParseAll [" "]
        HParseAll["**Parse all quota fields**"]
        HParseAll --> Return(["return info, nil"])
    end

    Pipeline["client.Pipeline()"]
    GetPercent["client.Get(ctx, quotaKey(userID, 'plan_percent'))"]
    GetPlanDollars["client.Get(ctx, quotaKey(userID, 'plan_dollars'))"]
    GetAddonDollars["client.Get(ctx, quotaKey(userID, 'addon_dollars'))"]
    GetReserved["client.Get(ctx, quotaKey(userID, 'reserved'))"]
    GetDays["client.Get(ctx, quotaKey(userID, 'days_until_reset'))"]
    Exec["pipe.Exec(ctx)"]
    ParseInt["strconv.ParseInt(str, 10, 64)"]
    Atoi["strconv.Atoi(str)"]
    CentsToDollars["centsToDollars(cents)"]

    click Pipeline href "https://pkg.go.dev/github.com/redis/go-redis/v9#Client.Pipeline"
    click GetPercent href "https://pkg.go.dev/github.com/redis/go-redis/v9#Client.Get"
    click GetPlanDollars href "https://pkg.go.dev/github.com/redis/go-redis/v9#Client.Get"
    click GetAddonDollars href "https://pkg.go.dev/github.com/redis/go-redis/v9#Client.Get"
    click GetReserved href "https://pkg.go.dev/github.com/redis/go-redis/v9#Client.Get"
    click GetDays href "https://pkg.go.dev/github.com/redis/go-redis/v9#Client.Get"
    click Exec href "https://pkg.go.dev/github.com/redis/go-redis/v9#Pipeline.Exec"
    click ParseInt href "https://pkg.go.dev/strconv#ParseInt"
    click Atoi href "https://pkg.go.dev/strconv#Atoi"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Return mainFn
    class Pipeline,GetPercent,GetPlanDollars,GetAddonDollars,GetReserved,GetDays,Exec,ParseInt,Atoi,CentsToDollars localPkgs
    class E1 errorFlow
```

### InitializeUserQuota

```mermaid
flowchart TB
    Start(["InitializeUserQuota(ctx, client, userID, planDollars, daysUntilReset)"]) --> HPipeline

    subgraph SGPipeline [" "]
        HPipeline["**pipe := client.Pipeline()**"]
        HPipeline --> HSetAll
    end

    subgraph SGSetAll [" "]
        HSetAll["**Set all quota fields atomically**"]
        HSetAll --> HExec
    end

    subgraph SGExec [" "]
        HExec["**pipe.Exec(ctx) → _, err**"]
        HExec -->|error| E1["return err"]
        HExec --> Return(["return nil"])
    end

    Pipeline["client.Pipeline()"]
    SetPercent["pipe.Set(ctx, quotaKey(userID, 'plan_percent'), 100, 0)"]
    SetPlanDollars["pipe.Set(ctx, quotaKey(userID, 'plan_dollars'), formatCents(planDollars), 0)"]
    SetAddonDollars["pipe.Set(ctx, quotaKey(userID, 'addon_dollars'), 0, 0)"]
    SetReserved["pipe.Set(ctx, quotaKey(userID, 'reserved'), 0, 0)"]
    SetDays["pipe.Set(ctx, quotaKey(userID, 'days_until_reset'), daysUntilReset, 0)"]
    Exec["pipe.Exec(ctx)"]
    FormatCents["formatCents(dollars) → cents"]

    click Pipeline href "https://pkg.go.dev/github.com/redis/go-redis/v9#Client.Pipeline"
    click SetPercent href "https://pkg.go.dev/github.com/redis/go-redis/v9#Pipeline.Set"
    click SetPlanDollars href "https://pkg.go.dev/github.com/redis/go-redis/v9#Pipeline.Set"
    click SetAddonDollars href "https://pkg.go.dev/github.com/redis/go-redis/v9#Pipeline.Set"
    click SetReserved href "https://pkg.go.dev/github.com/redis/go-redis/v9#Pipeline.Set"
    click SetDays href "https://pkg.go.dev/github.com/redis/go-redis/v9#Pipeline.Set"
    click Exec href "https://pkg.go.dev/github.com/redis/go-redis/v9#Pipeline.Exec"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Return mainFn
    class Pipeline,SetPercent,SetPlanDollars,SetAddonDollars,SetReserved,SetDays,Exec,FormatCents localPkgs
    class E1 errorFlow
```

### AddCredits

```mermaid
flowchart TB
    Start(["AddCredits(ctx, client, purchase)"]) --> HCalcFee

    subgraph SGCalcFee [" "]
        HCalcFee["**feeAmount := purchase.Amount * float64(purchase.FeePercent) / 100**"]
        HCalcFee --> HCalcNet["**netCredits := purchase.Amount - feeAmount**"]
        HCalcNet --> HIncrDollars
    end

    subgraph SGIncrDollars [" "]
        HIncrDollars["**client.IncrBy(ctx, key, formatCents(netCredits)).Err() → err**"]
        HIncrDollars -->|error| E1["return err"]
        HIncrDollars --> HIncrTotal
    end

    subgraph SGIncrTotal [" "]
        HIncrTotal["**client.IncrBy(ctx, totalKey, formatCents(netCredits))**"]
        HIncrTotal --> HSetTimestamp
    end

    subgraph SGSetTimestamp [" "]
        HSetTimestamp["**client.Set(ctx, lastKey, time.Now().Unix(), 0)**"]
        HSetTimestamp --> Return(["return nil"])
    end

    IncrBy["client.IncrBy(ctx, key, amount).Err() → err"]
    SetTime["client.Set(ctx, key, timestamp, 0)"]
    FormatCents["formatCents(dollars) → cents"]
    NowUnix["time.Now().Unix()"]

    click IncrBy href "https://pkg.go.dev/github.com/redis/go-redis/v9#Client.IncrBy"
    click SetTime href "https://pkg.go.dev/github.com/redis/go-redis/v9#Client.Set"
    click NowUnix href "https://pkg.go.dev/time#Now"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Return mainFn
    class IncrBy,SetTime,FormatCents,NowUnix localPkgs
    class E1 errorFlow
```

### GetAddonBalance

```mermaid
flowchart TB
    Start(["GetAddonBalance(ctx, client, userID)"]) --> HPipeline

    subgraph SGPipeline [" "]
        HPipeline["**pipe := client.Pipeline()**"]
        HPipeline --> HGetAll
    end

    subgraph SGGetAll [" "]
        HGetAll["**Get all addon balance fields**"]
        HGetAll --> HExec
    end

    subgraph SGExec [" "]
        HExec["**pipe.Exec(ctx) → _, err**"]
        HExec -->|error && !redis.Nil| E1["return nil, err"]
        HExec --> HParseAll
    end

    subgraph SGParseAll [" "]
        HParseAll["**Parse all addon balance fields**"]
        HParseAll --> Return(["return balance, nil"])
    end

    Pipeline["client.Pipeline()"]
    GetBalance["client.Get(ctx, quotaKey(userID, 'addon_dollars'))"]
    GetTotal["client.Get(ctx, quotaKey(userID, 'addon_total_added'))"]
    GetLast["client.Get(ctx, quotaKey(userID, 'addon_last_refill'))"]
    Exec["pipe.Exec(ctx)"]
    Int64["cmd.Int64()"]
    CentsToDollars["centsToDollars(cents)"]
    Unix["time.Unix(timestamp, 0)"]

    click Pipeline href "https://pkg.go.dev/github.com/redis/go-redis/v9#Client.Pipeline"
    click GetBalance href "https://pkg.go.dev/github.com/redis/go-redis/v9#Client.Get"
    click GetTotal href "https://pkg.go.dev/github.com/redis/go-redis/v9#Client.Get"
    click GetLast href "https://pkg.go.dev/github.com/redis/go-redis/v9#Client.Get"
    click Exec href "https://pkg.go.dev/github.com/redis/go-redis/v9#Pipeline.Exec"
    click Int64 href "https://pkg.go.dev/github.com/redis/go-redis/v9#Cmd.Int64"
    click Unix href "https://pkg.go.dev/time#Unix"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Return mainFn
    class Pipeline,GetBalance,GetTotal,GetLast,Exec,Int64,CentsToDollars,Unix localPkgs
    class E1 errorFlow
```

### DeductFromAddon

```mermaid
flowchart TB
    Start(["DeductFromAddon(ctx, client, userID, amount)"]) --> HGetBalance

    subgraph SGGetBalance [" "]
        HGetBalance["**client.Get(ctx, key).Int64() → cents, err**"]
        HGetBalance -->|redis.Nil| E1["return err"]
        HGetBalance -->|error| E2["return err"]
        HGetBalance --> HCheck
    end

    subgraph SGCheck [" "]
        HCheck["**cents < formatCents(amount)**"]
        HCheck -->|true| E3["return err"]
        HCheck -->|false| HDecr
    end

    subgraph SGDecr [" "]
        HDecr["**client.DecrBy(ctx, key, formatCents(amount)).Err() → err**"]
        HDecr -->|error| E4["return err"]
        HDecr --> Return(["return nil"])
    end

    GetInt64["client.Get(ctx, key).Int64() → cents, err"]
    DecrBy["client.DecrBy(ctx, key, amount).Err() → err"]
    FormatCents["formatCents(dollars) → cents"]

    click GetInt64 href "https://pkg.go.dev/github.com/redis/go-redis/v9#Cmd.Int64"
    click DecrBy href "https://pkg.go.dev/github.com/redis/go-redis/v9#Client.DecrBy"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Return mainFn
    class GetInt64,DecrBy,FormatCents localPkgs
    class E1,E2,E3,E4 errorFlow
```

### HasAddonBalance

```mermaid
flowchart TB
    Start(["HasAddonBalance(ctx, client, userID)"]) --> HGetInt64

    subgraph SGGetInt64 [" "]
        HGetInt64["**client.Get(ctx, key).Int64() → cents, err**"]
        HGetInt64 -->|error| Return(["return false"])
        HGetInt64 --> HCheck
    end

    subgraph SGCheck [" "]
        HCheck["**cents > 0**"]
        HCheck -->|true| ReturnTrue(["return true"])
        HCheck -->|false| Return(["return false"])
    end

    GetInt64["client.Get(ctx, key).Int64() → cents, err"]

    click GetInt64 href "https://pkg.go.dev/github.com/redis/go-redis/v9#Cmd.Int64"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2

    class Start,Return,ReturnTrue mainFn
    class GetInt64 localPkgs
```

### ClearAddonCredits

```mermaid
flowchart TB
    Start(["ClearAddonCredits(ctx, client, userID)"]) --> HSet

    subgraph SGSet [" "]
        HSet["**client.Set(ctx, key, 0, 0).Err() → err**"]
        HSet --> Return(["return err"])
    end

    SetZero["client.Set(ctx, key, 0, 0).Err() → err"]

    click SetZero href "https://pkg.go.dev/github.com/redis/go-redis/v9#Client.Set"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2

    class Start,Return mainFn
    class SetZero localPkgs
```

## Structs

### Service

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `Service` | `client` | `*redis.Client` | Redis client |

### CheckResult

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `CheckResult` | `HasQuota` | `bool` | Has sufficient quota |
| | `Source` | `string` | "plan" or "addon" |
| | `PlanPercent` | `int` | Plan usage percent |
| | `PlanDollars` | `float64` | Plan dollars remaining |
| | `AddonDollars` | `float64` | Addon dollars remaining |
| | `UsingAddon` | `bool` | Using addon credits |
| | `EstimatedCost` | `float64` | Estimated cost |

### UserQuotaInfo

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `UserQuotaInfo` | `PlanPercent` | `int` | Plan usage percent |
| | `PlanDollars` | `float64` | Plan dollars |
| | `AddonDollars` | `float64` | Addon dollars |
| | `Reserved` | `float64` | Reserved amount |
| | `DaysUntilReset` | `int` | Days until reset |

### AddonPurchase

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `AddonPurchase` | `UserID` | `string` | User identifier |
| | `Amount` | `float64` | Purchase amount |
| | `CreditAmount` | `float64` | Credit amount |
| | `FeePercent` | `int` | Fee percentage |
| | `Timestamp` | `time.Time` | Purchase time |

### AddonBalance

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `AddonBalance` | `UserID` | `string` | User identifier |
| | `Balance` | `float64` | Current balance |
| | `TotalAdded` | `float64` | Total added |
| | `LastRefill` | `time.Time` | Last refill time |

## Redis Keys

| Key Pattern | Description |
|-------------|-------------|
| `quota:{userID}:plan_dollars` | Plan credit balance |
| `quota:{userID}:addon_dollars` | Addon credit balance |
| `quota:{userID}:plan_percent` | Plan usage percentage |
| `quota:{userID}:reserved` | Reserved amount |
| `quota:{userID}:days_until_reset` | Days until reset |
| `quota:{userID}:addon_total_added` | Total addon credits added |
| `quota:{userID}:addon_last_refill` | Last addon refill timestamp |

## Redis Key Format

Values are stored as integer cents (multiplied by 1000) to support DecrBy operations:
- `formatCents(dollars float64) int64`: Convert dollars to cents
- `centsToDollars(cents int64) float64`: Convert cents to dollars

## Related

- [LLM Proxy README](README.md) - Main proxy documentation
- [Config Plans](../../../config/plans.md) - Plan definitions
