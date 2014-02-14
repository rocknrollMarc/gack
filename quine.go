package gack

import (
	"errors"

	"go/ast"
	"go/build"

	"fmt"
	"path"
	"strings"
	"syscall"

	"io/ioutil"

	"os"
	"os/exec"

	"github.com/0xfaded/eval"
)

func Quine(env *eval.SimpleEnv, imports, history []string) error {
	required := []string{
		"reflect",
		"os",
		"github.com/0xfaded/eval",
		"github.com/0xfaded/gack",
	}

	f, err := ioutil.TempFile("/tmp", "gack")
	if err != nil {
		return err
	}
	srcDeleted := false
	_ = srcDeleted
	// if the exec is successful this will never be run
	defer (func() {
		if !srcDeleted {
			f.Close()
			os.Remove(f.Name())
		}
	})()

	if _, err = fmt.Fprint(f, "package main\nimport (\n"); err != nil {
		return err
	}

	imported := map[string]bool{}
	names := map[string]string{}
	pkgs := make(map[string]*ast.Package, len(imports))
	for _, i := range(imports) {
		if absolute, clean, err := findImport(i); err != nil {
			return err
		} else if pkg, err := Import(absolute); err != nil {
			return err
		} else if at, ok := names[pkg.Name]; ok {
			return errors.New(fmt.Sprintf("%v redeclared as imported package name\n" +
				"\tprevious declaration at %v", pkg.Name, at))
		} else if _, err := fmt.Fprintf(f, "\t\"%s\"\n", clean); err != nil {
			return err
		} else {
			imported[clean] = true
			pkgs[clean] = pkg
			for f := range pkg.Files {
				names[pkg.Name] = f
				break
			}
		}
	}
	for _, r := range required {
		if !imported[r] {
			if _, err := fmt.Fprintf(f, "\t\"%s\"\n", r); err != nil {
				return err
			}
		}
	}

	if _, err := fmt.Fprint(f, ")\nfunc main() {\n"); err != nil {
		return err
	}

	if err := WriteEnv(f, env, pkgs); err != nil {
		return err
	}


	if _, err := fmt.Fprint(f, "\thistory := []string{}\n"); err != nil {
		return err
	}

	// Replay the previous session
	for _, h := range history {
		if _, err := fmt.Fprintf(f, "\teval.EvalEnv(%s, root)\n\thistory = append(history, %s)", h, h); err != nil {
			return err
		}
	}

	// Delete the previous binary
	if _, err := fmt.Fprintf(f, "\tos.Remove(\"%v\")\n", os.Args[0]); err != nil {
		return err
	}

	// Enter the repl
	if _, err := fmt.Fprint(f, "\tgack.Repl(root, history)\n}"); err != nil {
		return err
	}

	// Compile the new program
	o, err := ioutil.TempFile("/tmp", "gack")
	if err != nil {
		return err
	}
	o.Close()

	srcDeleted = true
	f.Close()

	compiler := path.Join(build.ToolDir, "8g")
	linker := path.Join(build.ToolDir, "8l")
	if strings.HasPrefix(build.Default.GOARCH, "amd64") {
		compiler = path.Join(build.ToolDir, "6g")
		linker = path.Join(build.ToolDir, "6l")
	}

	platform := build.Default.GOOS + "_" + build.Default.GOARCH
	gopathlibs := path.Join(os.Getenv("GOPATH"), "pkg", platform)
	gorootlibs := path.Join(os.Getenv("GOROOT"), "pkg", platform)
	cmd := exec.Command(compiler, "-o", o.Name(), "-I", gopathlibs, "-I", gorootlibs, f.Name())
	if output, err := cmd.Output(); err != nil {
		fmt.Fprintf(os.Stdout, "Generated src failed to compile. Please file a bug report " +
			"with %s attached\n", f.Name())
		os.Stdout.Write(output)
		return err
	}

	// Delete the generated source
	os.Remove(f.Name())

	e, err := ioutil.TempFile("/tmp", "gack")
	if err != nil {
		return err
	}
	e.Close()
	cmd = exec.Command(linker, "-o", e.Name(), "-L", gopathlibs, "-L", gorootlibs, o.Name())
	if output, err := cmd.Output(); err != nil {
		fmt.Fprintf(os.Stdout, "Generated src failed to compile. Please file a bug report " +
			"with %s attached\n", f.Name())
		os.Stdout.Write(output)
		return err
	}

	// Delete the object file
	os.Remove(o.Name())

	// Go for the kill :)
	return syscall.Exec(e.Name(), []string{o.Name()}, os.Environ())
}

func findImport(pkgPath string) (absolutePath, cleanedPkgPath string, err error) {
	// Spec allows unicode. Also, is there a better IsAscii somewhere?
	if len(pkgPath) == 0 || len(pkgPath) != len([]byte(pkgPath)) {
		return "", "", errors.New("bad package path: " + pkgPath)
	}
	parts := strings.Split(pkgPath, "/")
	if parts[0] == "" {
		return "", "", errors.New("cannot import absolute path: " + pkgPath)
	}

	for i := 0; i < len(parts); i += 1 {
		if parts[i] == "" {
			parts = append(parts[:i], parts[i+1:]...)
			i -= 1
		} else {
			parts[i] = strings.Trim(parts[i], " \n\t")
			if parts[i] == "" {
				return "", "", errors.New("bad import path: " + pkgPath)
			}
		}
	}
	clean := strings.Join(parts, "/")
	gopath := path.Join(os.Getenv("GOPATH"), "src", clean)
	if fi, _ := os.Stat(gopath); fi != nil && fi.IsDir() {
		return gopath, clean, nil
	}
	goroot := path.Join(os.Getenv("GOROOT"), "src", "pkg", clean)
	if fi, _ := os.Stat(goroot); fi != nil && fi.IsDir() {
		return goroot, clean, nil
	}
	return "", "", errors.New(fmt.Sprintf(`cannot find package "%s" in any of:
	%s (from $GOROOT)
	%s (from $GOPATH)`, clean, goroot, gopath))
}
