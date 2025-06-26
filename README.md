# agentflow
[![Godoc Reference](https://godoc.org/github.com/omniaura/agentflow?status.svg)](http://godoc.org/github.com/omniaura/agentflow)
[![Go Coverage](https://github.com/omniaura/agentflow/wiki/coverage.svg)](https://raw.githack.com/wiki/omniaura/agentflow/coverage.html)
[![Go Report Card](https://goreportcard.com/badge/github.com/omniaura/agentflow)](https://goreportcard.com/report/github.com/omniaura/agentflow)

A powerful template engine for AI prompt engineering with type-safe variable interpolation and conditional logic.

## Table of Contents

- [Installation](#installation)
- [Quick Start](#quick-start)
- [AgentFlow (.af) Syntax](#agentflow-af-syntax)
  - [Titles](#titles)
  - [Variables](#variables)
  - [Type System](#type-system)
  - [Conditional Blocks](#conditional-blocks)
  - [Nested Variables](#nested-variables)
  - [Comments and Text](#comments-and-text)
- [Examples](#examples)
  - [Basic Variables](#basic-variables)
  - [Typed Variables](#typed-variables)
  - [Conditional Prompts](#conditional-prompts)
  - [Complex Nested Structures](#complex-nested-structures)
- [Generated Code](#generated-code)
- [CLI Usage](#cli-usage)

## Installation

    go install github.com/omniaura/agentflow/cmd/af@latest

As of go 1.24 you can now use `af` as a go tool:

    go get -tool github.com/omniaura/agentflow/cmd/af@latest

This installs `af` scoped to your go project and you call it using `go tool af`

## Quick Start

Create a simple prompt template:

```af
.title Greeting
Hello <!name>! Welcome to <!platform>.

.title Personalized Message  
<?premium bool>
Thanks for being a premium user! You have access to all features.
<else>
Consider upgrading to premium for additional features.
</premium>
```

Generate Go code:

    af gen prompts examples/

## AgentFlow (.af) Syntax

AgentFlow uses a simple but powerful syntax for creating dynamic prompt templates with type-safe variable interpolation.

### Titles

Titles define distinct prompt sections within a file. Each title becomes a separate Go function.

```af
.title System Prompt
You are a helpful assistant.

.title User Greeting
Hello <!user_name>, how can I help you today?
```

**Generated Go:**
```go
type SystemPrompt struct{}
func (input *SystemPrompt) String() string { /* ... */ }

type UserGreeting struct {
    UserName string
}
func (input *UserGreeting) String() string { /* ... */ }
```

### Variables

Variables are interpolated using the `<!variable_name>` syntax:

```af
Hello <!name>! You have <!count> messages.
```

Variables support dot notation for nested structures:

```af
Welcome <!user.name>! Your email is <!user.email>.
```

### Type System

AgentFlow supports a rich type system with explicit type annotations and intelligent type caching.

#### Supported Types
- `string` (default)
- `int` 
- `bool`
- `float32`
- `float64`

#### Type Declaration and Caching

Variables can be declared with explicit types on first use:

```af
User <!username> has <!count int> messages and is <!active bool> status.
Message Count: <!count>
Active Status: <!active>
```

**Key Features:**
- **Type Declaration**: `<!count int>` - declares variable with type
- **Type Caching**: `<!count>` - subsequent references reuse the cached type
- **Automatic Conversion**: Generates appropriate `strconv.Itoa()` and `strconv.FormatBool()` calls

**Generated Go:**
```go
type Example struct {
    Username string
    Count    int      // ✅ Correctly typed
    Active   bool     // ✅ Correctly typed  
}

func (input *Example) String() string {
    var b strings.Builder
    var0 := strconv.Itoa(input.Count)  // ✅ Efficient reuse
    // ... both references use var0
}
```

### Conditional Blocks

Conditional blocks allow content to be included based on variable values:

```af
<?user.premium bool>
Welcome premium user! You have access to all features.
Your tier: <!user.subscription.tier int>.
<else>
Welcome! Upgrade to premium for more features.
</user.premium>
```

**Syntax:**
- `<?variable_name>` - Start conditional block
- `<else>` - Alternative content (optional)
- `</variable_name>` - End conditional block

**Conditional Logic:**
- `string` variables: included if not empty (`!= ""`)
- `int` variables: included if not zero (`!= 0`) 
- `bool` variables: included if true

### Nested Variables

AgentFlow supports complex nested data structures:

```af
<?memory.long>
## Long Term Memory
<!memory.long>
</memory.long>

<?script.name>
Working on: <!script.name> (<!script.type> project)
</script.name>
```

**Generated Go struct:**
```go
type Template struct {
    Memory struct {
        Long  string
        Short string
    }
    Script struct {
        Name string
        Type string
    }
}
```

### Comments and Text

All text outside of special syntax is treated as literal content. AgentFlow preserves whitespace and formatting exactly as written.

```af
This is literal text that will be included as-is.

Variables like <!name> are interpolated.
But this <text> is just literal text.
```

## Examples

### Basic Variables

```af
.title Simple Greeting
Hello <!name>! Today is <!date>.
```

### Typed Variables

```af
.title User Stats
User <!username> has <!message_count int> messages.
Premium status: <!is_premium bool>
Account balance: $<!balance float64>
```

### Conditional Prompts

```af
.title Welcome Message
<?user.premium bool>
🌟 Welcome back, premium user <!user.name>!
You have access to all premium features.
<else>
Hello <!user.name>! Consider upgrading to premium.
</user.premium>

<?notifications int>
You have <!notifications> new notifications.
</notifications>
```

### Complex Nested Structures

```af
.title AI Assistant
You are <!ai.name>, an AI assistant.

<?user.preferences.language>
Respond in <!user.preferences.language>.
</user.preferences.language>

<?context.previous_messages>
Previous conversation:
<!context.previous_messages>
</context.previous_messages>

Current user: <!user.name>
Timestamp: <!timestamp>
```

## Generated Code

AgentFlow generates efficient, type-safe Go code with:

- **Two-pass string building** for optimal performance
- **Proper type handling** with automatic conversions
- **Conditional logic** that mirrors your template structure
- **Variable reuse** to minimize allocations

Example generated code:
```go
func (input *Template) String() string {
    var b strings.Builder
    var0 := strconv.Itoa(input.Count)
    length := 0
    length += 5  // "User "
    length += len(input.Username)
    length += 5  // " has "
    length += len(var0)
    length += 10 // " messages."
    b.Grow(length)
    b.WriteString(`User `)
    b.WriteString(input.Username)
    b.WriteString(` has `)
    b.WriteString(var0)
    b.WriteString(` messages.`)
    return b.String()
}
```

## CLI Usage

Generate Go code from .af templates:

    af gen prompts <directory>

**Options:**
- `-d, --dir`: Directory containing .af files (default: current directory)
- `-l, --lang`: Target language (currently only 'go' supported)

**Example:**
    af gen prompts examples/simple
