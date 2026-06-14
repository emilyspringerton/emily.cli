// cmd/train.go — emily train
//
// Agent command for the GPT-2 fine-tuning pipeline.
// Orchestrates: dataset build → Drive upload → status check.
//
// Usage:
//   emily train build-dataset [--emily-root <path>] [--output <path>] [--mode lm|instruct]
//   emily train upload <file> [<file>...]
//   emily train status

package cmd

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/emilyspringerton/emily-cli/internal/config"
	"github.com/emilyspringerton/emily-cli/internal/iduna"
)

func RunTrain(args []string) int {
	if len(args) == 0 {
		printTrainUsage()
		return 1
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "build-dataset":
		return runTrainBuildDataset(rest)
	case "upload":
		return runTrainUpload(rest)
	case "status":
		return runTrainStatus(rest)
	default:
		fmt.Fprintf(os.Stderr, "emily train: unknown subcommand %q\n", sub)
		printTrainUsage()
		return 1
	}
}

func printTrainUsage() {
	fmt.Fprintln(os.Stderr, `usage: emily train <subcommand> [flags]

Subcommands:
  build-dataset   Build JSONL training corpus from Emily golden docs
  upload <file>   Upload training artifact to Drive via IDUNA
  status          Show Drive files + training pipeline state

Flags for build-dataset:
  --emily-root <path>   Path to EMILY repo (default: auto-discover)
  --output <path>       Output JSONL file (default: /tmp/emily-corpus.jsonl)
  --mode lm|instruct    Training mode (default: lm)
  --gpt2-root <path>    Path to gpt2-alpine-c repo (for script location)

Environment:
  IDUNA_BASE_URL, IDUNA_AGENT_NAME (EMILY-TRAINING), IDUNA_AGENT_SECRET`)
}

func runTrainBuildDataset(args []string) int {
	fs := flag.NewFlagSet("train build-dataset", flag.ContinueOnError)
	emilyRoot := fs.String("emily-root", "", "path to EMILY repo (auto-discover if empty)")
	output := fs.String("output", "/tmp/emily-corpus.jsonl", "output JSONL path")
	mode := fs.String("mode", "lm", "training mode: lm or instruct")
	gpt2Root := fs.String("gpt2-root", "", "path to gpt2-alpine-c repo")
	verbose := fs.Bool("v", false, "verbose output")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}

	if *emilyRoot == "" {
		*emilyRoot = cfg.EmilyRoot
	}
	if *emilyRoot == "" {
		fmt.Fprintln(os.Stderr, "error: cannot find EMILY root; set --emily-root or EMILY_ROOT env")
		return 1
	}

	if *gpt2Root == "" {
		*gpt2Root = filepath.Join(filepath.Dir(*emilyRoot), "gpt2-alpine-c")
	}

	scriptPath := filepath.Join(*gpt2Root, "scripts", "prime_directive_dataset.py")
	if _, err := os.Stat(scriptPath); err != nil {
		fmt.Fprintf(os.Stderr, "error: script not found: %s\n", scriptPath)
		fmt.Fprintln(os.Stderr, "  Clone gpt2-alpine-c and set --gpt2-root")
		return 1
	}

	cmdArgs := []string{
		scriptPath,
		"--emily-root", *emilyRoot,
		"--output", *output,
		"--mode", *mode,
	}
	if *verbose {
		cmdArgs = append(cmdArgs, "--verbose")
	}

	fmt.Printf("◈ Building training corpus\n")
	fmt.Printf("  Emily root: %s\n", *emilyRoot)
	fmt.Printf("  Output:     %s\n", *output)
	fmt.Printf("  Mode:       %s\n\n", *mode)

	cmd := exec.Command("python3", cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "build-dataset failed: %v\n", err)
		return 1
	}

	fmt.Printf("\n✓ Corpus built: %s\n", *output)
	fmt.Printf("  Next: emily train upload %s\n", *output)
	return 0
}

func runTrainUpload(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: emily train upload <file> [<file>...]")
		return 1
	}

	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}

	agentName := os.Getenv("IDUNA_AGENT_NAME")
	if agentName == "" {
		agentName = "EMILY-TRAINING"
	}
	agentSecret := os.Getenv("IDUNA_AGENT_SECRET")
	if agentSecret == "" {
		agentSecret = cfg.IDUNAAgentSecret
	}

	if agentSecret == "" {
		fmt.Fprintln(os.Stderr, "error: IDUNA_AGENT_SECRET not set")
		fmt.Fprintln(os.Stderr, "  source IDUNA/var/agent-secrets.env or set IDUNA_AGENT_SECRET")
		return 1
	}

	client := iduna.New(cfg.IDUNABaseURL, agentName, agentSecret)

	mimeMap := map[string]string{
		".jsonl": "application/x-ndjson",
		".json":  "application/json",
		".txt":   "text/plain",
		".py":    "text/x-python",
		".bin":   "application/octet-stream",
	}

	for _, filePath := range args {
		data, err := os.ReadFile(filePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error reading %s: %v\n", filePath, err)
			return 1
		}

		filename := filepath.Base(filePath)
		ext := strings.ToLower(filepath.Ext(filename))
		mime, ok := mimeMap[ext]
		if !ok {
			mime = "application/octet-stream"
		}

		sizeKB := float64(len(data)) / 1024
		fmt.Printf("Uploading %s (%.1f KB)...\n", filename, sizeKB)

		result, err := client.DriveUpload(filename, mime, data)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  upload failed: %v\n", err)
			return 1
		}
		fmt.Printf("  ✓ id=%s  link=%s\n", result.ID, result.WebViewLink)
	}

	fmt.Printf("\n✓ Upload complete\n")
	fmt.Printf("  Next: open the Colab notebook and load the dataset from Drive\n")
	return 0
}

func runTrainStatus(args []string) int {
	_ = args

	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}

	agentName := os.Getenv("IDUNA_AGENT_NAME")
	if agentName == "" {
		agentName = "EMILY-TRAINING"
	}
	agentSecret := os.Getenv("IDUNA_AGENT_SECRET")
	if agentSecret == "" {
		agentSecret = cfg.IDUNAAgentSecret
	}

	fmt.Printf("◈ EMILY TRAINING STATUS | %s\n\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Printf("  IDUNA:       %s\n", cfg.IDUNABaseURL)
	fmt.Printf("  Agent:       %s\n", agentName)
	fmt.Printf("  Drive API:   %s/api/v1/drive/files\n\n", cfg.IDUNABaseURL)

	if agentSecret == "" {
		fmt.Println("  Drive files: (IDUNA_AGENT_SECRET not set — cannot authenticate)")
		return 0
	}

	client := iduna.New(cfg.IDUNABaseURL, agentName, agentSecret)
	files, err := client.DriveList()
	if err != nil {
		fmt.Printf("  Drive files: error — %v\n", err)
		fmt.Println("  (Is IDUNA running? Is GOOGLE_DRIVE_SERVICE_ACCOUNT_JSON set?)")
		return 0
	}

	if len(files) == 0 {
		fmt.Println("  Drive files: (none)")
	} else {
		fmt.Printf("  Drive files (%d):\n", len(files))
		fmt.Printf("    %-40s  %10s  %s\n", "Name", "Size", "Created")
		fmt.Println("    " + strings.Repeat("-", 70))
		for _, f := range files {
			size := ""
			if f.Size != "" {
				if n, err := strconv.ParseInt(f.Size, 10, 64); err == nil {
					size = fmt.Sprintf("%.1f KB", float64(n)/1024)
				}
			}
			created := f.CreatedTime
			if len(created) > 10 {
				created = created[:10]
			}
			fmt.Printf("    %-40s  %10s  %s\n", truncateName(f.Name, 40), size, created)
		}
	}

	fmt.Printf("\n  Pipeline steps:\n")
	fmt.Printf("    1. build-dataset  emily train build-dataset --output /tmp/emily-corpus.jsonl\n")
	fmt.Printf("    2. upload         emily train upload /tmp/emily-corpus.jsonl\n")
	fmt.Printf("    3. colab          open notebooks/gpt2_finetune_colab.ipynb\n")
	fmt.Printf("    4. convert        python3 scripts/convert_ft_checkpoint.py --checkpoint checkpoint-final\n")
	fmt.Printf("    5. test           ./gpt2_run weights/emily-ft.bin --entropy-stats\n")

	return 0
}

func truncateName(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
