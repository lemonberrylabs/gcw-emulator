package stdlib

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/lemonberrylabs/gcw-emulator/pkg/types"
)

// registerSys registers sys.* functions.
func (r *Registry) registerSys() {
	r.Register("sys.get_env", sysGetEnv)
	r.Register("sys.log", sysLog)
	r.Register("sys.now", sysNow)
	r.Register("sys.sleep", sysSleep)
	r.Register("sys.sleep_until", sysSleepUntil)
}

func sysGetEnv(args []types.Value) (types.Value, error) {
	if len(args) == 0 {
		return types.Null, fmt.Errorf("sys.get_env requires a name argument")
	}

	// Can be called with positional arg or map arg
	var name string
	if args[0].Type() == types.TypeString {
		name = args[0].AsString()
	} else if args[0].Type() == types.TypeMap {
		nameVal, ok := args[0].AsMap().Get("name")
		if !ok {
			return types.Null, fmt.Errorf("sys.get_env requires 'name' argument")
		}
		name = nameVal.AsString()
	} else {
		return types.Null, types.NewTypeError("sys.get_env: name must be a string")
	}

	// GCW built-in environment variables with emulator defaults
	switch name {
	case "GOOGLE_CLOUD_PROJECT_ID":
		return types.NewString(envOrDefault("GOOGLE_CLOUD_PROJECT_ID", "emulator-project")), nil
	case "GOOGLE_CLOUD_PROJECT_NUMBER":
		return types.NewString(envOrDefault("GOOGLE_CLOUD_PROJECT_NUMBER", "000000000000")), nil
	case "GOOGLE_CLOUD_LOCATION":
		return types.NewString(envOrDefault("GOOGLE_CLOUD_LOCATION", "us-central1")), nil
	case "GOOGLE_CLOUD_WORKFLOW_ID":
		return types.NewString(envOrDefault("GOOGLE_CLOUD_WORKFLOW_ID", "emulator-workflow")), nil
	case "GOOGLE_CLOUD_WORKFLOW_REVISION_ID":
		return types.NewString(envOrDefault("GOOGLE_CLOUD_WORKFLOW_REVISION_ID", "000001-000")), nil
	case "GOOGLE_CLOUD_WORKFLOW_EXECUTION_ID":
		return types.NewString(envOrDefault("GOOGLE_CLOUD_WORKFLOW_EXECUTION_ID", "emulator-exec-1")), nil
	case "GOOGLE_CLOUD_WORKFLOW_EXECUTION_ATTEMPT":
		return types.NewString(envOrDefault("GOOGLE_CLOUD_WORKFLOW_EXECUTION_ATTEMPT", "1")), nil
	default:
		val := os.Getenv(name)
		if val == "" {
			return types.Null, types.NewKeyError(
				fmt.Sprintf("environment variable '%s' not found", name))
		}
		return types.NewString(val), nil
	}
}

func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func sysLog(args []types.Value) (types.Value, error) {
	if len(args) == 0 {
		return types.Null, nil
	}

	var data types.Value
	severity := "DEFAULT"

	if args[0].Type() == types.TypeMap {
		// Map-style args from call step
		m := args[0].AsMap()
		if d, ok := m.Get("data"); ok {
			data = d
		} else if d, ok := m.Get("text"); ok {
			data = d
		} else {
			data = args[0]
		}
		if s, ok := m.Get("severity"); ok && s.Type() == types.TypeString {
			severity = s.AsString()
		}
	} else {
		data = args[0]
		if len(args) > 1 && args[1].Type() == types.TypeString {
			severity = args[1].AsString()
		}
	}

	log.Printf("[%s] %s", severity, data.String())
	return types.Null, nil
}

func sysNow(args []types.Value) (types.Value, error) {
	return types.NewDouble(float64(time.Now().Unix())), nil
}

func sysSleep(args []types.Value) (types.Value, error) {
	if len(args) == 0 {
		return types.Null, nil
	}

	var seconds float64
	if args[0].Type() == types.TypeMap {
		if s, ok := args[0].AsMap().Get("seconds"); ok {
			n, ok := s.AsNumber()
			if !ok {
				return types.Null, types.NewTypeError("sys.sleep: seconds must be a number")
			}
			seconds = n
		}
	} else {
		n, ok := args[0].AsNumber()
		if !ok {
			return types.Null, types.NewTypeError("sys.sleep: seconds must be a number")
		}
		seconds = n
	}

	// In emulator mode, we use a shorter sleep to speed up tests
	// but still sleep for at least a token amount
	duration := time.Duration(seconds * float64(time.Second))
	if duration > time.Second {
		duration = time.Second // cap at 1s in emulator
	}
	time.Sleep(duration)

	return types.Null, nil
}

func sysSleepUntil(args []types.Value) (types.Value, error) {
	// In emulator mode, this is effectively a no-op or very short sleep
	return types.Null, nil
}
