package cmdimg

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// Run now uses cobra for command handling and completions.
// It expects args similar to os.Args (program name at index 0).
// Callers typically pass os.Args as before.
func Run(ctx context.Context, args []string) error {
	// Drop program name if present
	if len(args) > 0 {
		args = args[1:]
	}

	root := &cobra.Command{
		Use:   "img",
		Short: "Image helper: convert/resize to WebP (ports legacy img script)",
		Long: `Port of the legacy img helper.

Subcommands:
  convert <file>              Convert a single image to <basename>.webp
  convert-all                 Convert all jpg/png files in cwd to webp (requires fd)
  resize --file <file> --width <width> --height <height> [cwebp args...]
							 Resize an image and write <basename>-w{width}-h{height}.{ext}
  normalize <file> [file... ] Normalize one or more filenames: lowercase, spaces -> -, collapse repeated -
  normalize-all               Normalize all files in cwd (non-recursive)
  (no subcommand)             Any args are forwarded to cwebp
`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, cmdArgs []string) error {
			// If no args: show help/usage
			if len(cmdArgs) == 0 {
				return cmd.Usage()
			}
			// Forward all args to cwebp
			if _, err := exec.LookPath("cwebp"); err != nil {
				return fmt.Errorf("cwebp is required but not found in PATH")
			}
			return runCmd(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), "cwebp", cmdArgs...)
		},
	}

	// convert
	convertCmd := &cobra.Command{
		Use:   "convert <file>",
		Short: "Convert a single image to <basename>.webp",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, cmdArgs []string) error {
			if _, err := exec.LookPath("cwebp"); err != nil {
				return fmt.Errorf("cwebp is required but not found in PATH")
			}
			return imgConvert(
				cmd.Context(),
				cmd.OutOrStdout(),
				cmd.ErrOrStderr(),
				cmdArgs...,
			)
		},
	}

	// convert-all
	convertAllCmd := &cobra.Command{
		Use:   "convert-all",
		Short: "Convert all jpg/png files in cwd to webp (requires fd)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, cmdArgs []string) error {
			if _, err := exec.LookPath("cwebp"); err != nil {
				return fmt.Errorf("cwebp is required but not found in PATH")
			}
			if _, err := exec.LookPath("fd"); err != nil {
				return fmt.Errorf("convert-all requires 'fd' in PATH")
			}
			return imgConvertAll(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	// resize - required parameters moved to flags: --file --width --height
	resizeCmd := &cobra.Command{
		Use:   "resize --file <file> --width <width> --height <height> [cwebp args...]",
		Short: "Resize an image and write <basename>-w{width}-h{height}.{ext}",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, cmdArgs []string) error {
			if _, err := exec.LookPath("cwebp"); err != nil {
				return fmt.Errorf("cwebp is required but not found in PATH")
			}
			file, _ := cmd.Flags().GetString("file")
			width, _ := cmd.Flags().GetInt("width")
			height, _ := cmd.Flags().GetInt("height")

			// positional args after flags are passed as extra cwebp args
			extra := []string{}
			if len(cmdArgs) > 0 {
				extra = cmdArgs
			}

			return imgResize(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), file, strconv.Itoa(width), strconv.Itoa(height), extra...)
		},
	}
	resizeCmd.Flags().String("file", "", "input file to resize")
	resizeCmd.Flags().Int("width", 0, "width to resize to (0 to auto)")
	resizeCmd.Flags().Int("height", 0, "height to resize to (0 to auto)")
	_ = resizeCmd.MarkFlagRequired("file")
	_ = resizeCmd.MarkFlagRequired("width")
	_ = resizeCmd.MarkFlagRequired("height")
	// Provide completions for --file flag restricted to common image extensions
	_ = resizeCmd.MarkFlagFilename("file", "jpg", "jpeg", "png", "gif", "webp", "bmp", "tiff", "tif", "avif", "heic")

	// normalize (one or more files)
	normalizeCmd := &cobra.Command{
		Use:   "normalize <file> [file...]",
		Short: "Normalize one or more filenames: lowercase, spaces->-, collapse repeated -",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, cmdArgs []string) error {
			del, _ := cmd.Flags().GetBool("delete")
			return imgNormalizeMany(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), cmdArgs, del)
		},
	}
	normalizeCmd.Flags().Bool("delete", false, "remove original files after creating normalized copy")

	// normalize-all
	normalizeAllCmd := &cobra.Command{
		Use:   "normalize-all",
		Short: "Normalize all files in cwd (non-recursive)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, cmdArgs []string) error {
			del, _ := cmd.Flags().GetBool("delete")
			return imgNormalizeAll(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), del)
		},
	}
	normalizeAllCmd.Flags().Bool("delete", false, "remove original files after creating normalized copy")

	// completion command to generate shell completion scripts
	completionCmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate shell completion script",
		Long: `Generate shell completion script for bash, zsh, fish or powershell.
	Example:
	  img completion bash > /etc/bash_completion.d/img
	`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, cmdArgs []string) error {
			switch cmdArgs[0] {
			case "bash":
				return cmd.Root().GenBashCompletion(cmd.OutOrStdout())
			case "zsh":
				// GenZshCompletion writes zsh completion header unless noDesc is true.
				return cmd.Root().GenZshCompletion(cmd.OutOrStdout())
			case "fish":
				return cmd.Root().GenFishCompletion(cmd.OutOrStdout(), true)
			case "powershell":
				return cmd.Root().GenPowerShellCompletionWithDesc(cmd.OutOrStdout())
			default:
				return fmt.Errorf("unsupported shell: %s", cmdArgs[0])
			}
		},
	}

	root.AddCommand(convertCmd, convertAllCmd, resizeCmd, normalizeCmd, normalizeAllCmd, completionCmd)

	// Set args for cobra and execute with provided context
	root.SetArgs(args)
	// Ensure cobra uses the provided context when running commands
	if err := root.ExecuteContext(ctx); err != nil {
		return err
	}
	return nil
}

func imgConvert(ctx context.Context, out, errOut io.Writer, files ...string) error {
	for _, file := range files {
		// Check file exists
		if fi, err := os.Stat(file); err != nil || fi.IsDir() {
			return fmt.Errorf("file does not exist or is a directory: %s", file)
		}
		ext := "webp"
		base := file[:len(file)-len(filepath.Ext(file))]
		outPath := fmt.Sprintf("%s.%s", base, ext)

		if err := runCmd(ctx, out, errOut, "cwebp", file, "-o", outPath); err != nil {
			return fmt.Errorf("cwebp conversion failed: %w", err)
		}
		fmt.Fprintf(out, "Successfully converted '%s' to '%s'\n", file, outPath)
	}
	return nil
}

func imgConvertAll(ctx context.Context, out, errOut io.Writer) error {
	// Use fd to convert jpg and png using fd's replacement patterns.
	cmdStrJpg := `fd . -e jpg --no-ignore -x cwebp "{}" -o "{.}.webp"`
	cmdStrPng := `fd . -e png --no-ignore -x cwebp "{}" -o "{.}.webp"`

	if err := runShell(ctx, out, errOut, cmdStrJpg); err != nil {
		return fmt.Errorf("converting jpg files failed: %w", err)
	}
	if err := runShell(ctx, out, errOut, cmdStrPng); err != nil {
		return fmt.Errorf("converting png files failed: %w", err)
	}
	return nil
}

func imgResize(ctx context.Context, out, errOut io.Writer, file, wStr, hStr string, extraArgs ...string) error {
	// Validate file
	if fi, err := os.Stat(file); err != nil || fi.IsDir() {
		return fmt.Errorf("file does not exist or is a directory: %s", file)
	}

	w, err := strconv.Atoi(wStr)
	if err != nil {
		return fmt.Errorf("width is not a valid number: %s", wStr)
	}
	h, err := strconv.Atoi(hStr)
	if err != nil {
		return fmt.Errorf("height is not a valid number: %s", hStr)
	}
	if w < 0 || h < 0 {
		return errors.New("width and height must be non-negative")
	}

	// create a temp output file
	tmpFile, err := os.CreateTemp("", "img_resize_*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	// Ensure removal of tmp file on exit
	defer os.Remove(tmpPath)

	// Build cwebp args: -resize w h <in> -o <tmp>
	args := []string{"-resize", strconv.Itoa(w), strconv.Itoa(h), file, "-o", tmpPath}
	if len(extraArgs) > 0 {
		args = append(args, extraArgs...)
	}

	if err := runCmd(ctx, out, errOut, "cwebp", args...); err != nil {
		return fmt.Errorf("cwebp resize failed: %w", err)
	}

	// Determine dimensions if either w or h is zero
	finalW := w
	finalH := h
	if w == 0 || h == 0 {
		dw, dh, derr := getImageDimensions(ctx, tmpPath)
		if derr != nil {
			// if we can't detect dims, leave zeros as-is but still write file
			fmt.Fprintf(errOut, "warning: failed to determine dimensions: %v\n", derr)
		} else {
			if w == 0 {
				finalW = dw
			}
			if h == 0 {
				finalH = dh
			}
		}
	}

	ext := filepath.Ext(file)
	if ext == "" {
		ext = ".webp"
	}
	ext = ext[1:] // remove leading dot

	base := file[:len(file)-len(filepath.Ext(file))]
	outPath := fmt.Sprintf("%s-w%d-h%d.%s", base, finalW, finalH, ext)

	// copy tmpPath to out
	if err := copyFile(tmpPath, outPath); err != nil {
		return fmt.Errorf("failed to write output file: %w", err)
	}

	fmt.Fprintf(out, "Successfully resized '%s' to '%s'\n", file, outPath)
	return nil
}

// imgNormalize normalizes a single filename according to rules:
// - make filename lowercase
// - replace spaces with '-'
// - collapse repeated '-' into a single '-'
// The file's extension is preserved (and lowercased). Operates on the provided path.
// By default this will NOT move the original file; it will create a normalized copy.
// If deleteOrig is true, the original file will be removed after the copy.
func imgNormalize(ctx context.Context, out, errOut io.Writer, path string, deleteOrig bool) error {
	// Check file exists and is not a directory
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("file does not exist: %s", path)
	}
	if fi.IsDir() {
		return fmt.Errorf("path is a directory, expected a file: %s", path)
	}

	dir := filepath.Dir(path)
	name := filepath.Base(path)
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)

	normalizedBase := normalizeName(base)
	extLower := strings.ToLower(ext)

	// if extension is empty, don't append dot
	newName := normalizedBase
	if extLower != "" {
		// ensure extLower includes leading dot
		if !strings.HasPrefix(extLower, ".") {
			extLower = "." + extLower
		}
		newName = normalizedBase + extLower
	}

	newPath := filepath.Join(dir, newName)
	// if name unchanged, nothing to do
	if newPath == path {
		fmt.Fprintf(out, "No change: '%s'\n", path)
		return nil
	}

	// if target exists, do not overwrite
	if _, err := os.Stat(newPath); err == nil {
		return fmt.Errorf("target already exists, skipping normalize: %s", newPath)
	}

	// Create normalized copy instead of moving by default
	if err := copyFile(path, newPath); err != nil {
		return fmt.Errorf("failed to create normalized file '%s' from '%s': %w", newPath, path, err)
	}

	if deleteOrig {
		if err := os.Remove(path); err != nil {
			// If deletion fails, attempt to remove the new file to avoid partial state? Just report error.
			return fmt.Errorf("created '%s' but failed to remove original '%s': %w", newPath, path, err)
		}
		fmt.Fprintf(out, "Renamed '%s' -> '%s'\n", path, newPath)
	} else {
		fmt.Fprintf(out, "Created '%s' from '%s'\n", newPath, path)
	}
	return nil
}

// imgNormalizeMany normalizes multiple files and aggregates errors.
func imgNormalizeMany(ctx context.Context, out, errOut io.Writer, files []string, deleteOrig bool) error {
	var errs []string
	for _, f := range files {
		if err := imgNormalize(ctx, out, errOut, f, deleteOrig); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", f, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("normalize completed with errors:\n%s", strings.Join(errs, "\n"))
	}
	return nil
}

// imgNormalizeAll normalizes all regular files in the current directory (non-recursive).
func imgNormalizeAll(ctx context.Context, out, errOut io.Writer, deleteOrig bool) error {
	entries, err := os.ReadDir(".")
	if err != nil {
		return fmt.Errorf("reading current directory failed: %w", err)
	}

	var errs []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if err := imgNormalize(ctx, out, errOut, name, deleteOrig); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", name, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("normalize-all completed with errors:\n%s", strings.Join(errs, "\n"))
	}
	return nil
}

// normalizeName applies the normalization rules to a filename base (without extension).
func normalizeName(s string) string {
	// lowercase
	out := strings.ToLower(s)
	// replace whitespace (one or more) with single hyphen
	spaceRe := regexp.MustCompile(`\s+`)
	out = spaceRe.ReplaceAllString(out, "-")
	// replace literal spaces just in case
	out = strings.ReplaceAll(out, " ", "-")
	// collapse repeated hyphens
	dupHyphenRe := regexp.MustCompile(`-+`)
	out = dupHyphenRe.ReplaceAllString(out, "-")
	// trim leading/trailing hyphens
	out = strings.Trim(out, "-")
	if out == "" {
		// fallback to a safe name
		return "file"
	}
	return out
}

func runCmd(ctx context.Context, stdout, stderr io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	if stdout != nil {
		cmd.Stdout = stdout
	} else {
		cmd.Stdout = os.Stdout
	}
	if stderr != nil {
		cmd.Stderr = stderr
	} else {
		cmd.Stderr = os.Stderr
	}
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func runShell(ctx context.Context, stdout, stderr io.Writer, cmdStr string) error {
	cmd := exec.CommandContext(ctx, "bash", "-lc", cmdStr)
	if stdout != nil {
		cmd.Stdout = stdout
	} else {
		cmd.Stdout = os.Stdout
	}
	if stderr != nil {
		cmd.Stderr = stderr
	} else {
		cmd.Stderr = os.Stderr
	}
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func getImageDimensions(ctx context.Context, path string) (int, int, error) {
	// Use the `file` utility and parse "123x456" style output.
	out, err := exec.CommandContext(ctx, "file", path).Output()
	if err != nil {
		return 0, 0, fmt.Errorf("file command failed: %w", err)
	}
	s := string(out)

	// Generic regexp to find NxM (e.g., 800x600)
	re := regexp.MustCompile(`([0-9]{1,5})x([0-9]{1,5})`)
	m := re.FindStringSubmatch(s)
	if len(m) < 3 {
		// Fallback to ImageMagick `identify -format %wx%h` if available
		if _, lookErr := exec.LookPath("identify"); lookErr == nil {
			out2, err2 := exec.CommandContext(ctx, "identify", "-format", "%wx%h", path).Output()
			if err2 == nil {
				m2 := re.FindStringSubmatch(string(out2))
				if len(m2) >= 3 {
					w, _ := strconv.Atoi(m2[1])
					h, _ := strconv.Atoi(m2[2])
					return w, h, nil
				}
			}
		}
		return 0, 0, fmt.Errorf("failed to parse dimensions from file output: %s", s)
	}
	w, _ := strconv.Atoi(m[1])
	h, _ := strconv.Atoi(m[2])
	return w, h, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() {
		_ = out.Close()
	}()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	// Ensure data is flushed to disk
	if err := out.Sync(); err != nil {
		return err
	}
	return nil
}
