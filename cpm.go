package main

/*
	CPM - C/C++ Package Manager
	(c) Mircea Neacsu 2021

	This tool uses simple JSON files to manage dependencies between
	different projects.

	Usage:
		cpm [options] [<project>]

	If project neame is missing, the program assumes to be the current
	directory.

	Valid options are:
		-f fetch-only (do not build)
		-v verbose

	The program opens the '${DEV_ROOT}/<project>/cpm.json' file and
	recursively searches for all dependencies.

	${DEV_ROOT} environment variable is the root of development tree.

*/
import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"

	"github.com/go-git/go-git/v5"
)

type BuildSet struct {
	Os   string
	Cmd  string
	Args []string
}

type PacUnit struct {
	Name    string
	Git     string
	Build   []*BuildSet
	Depends []*PacUnit
}

var devroot string         //root of development tree
var root_descriptor string //path of root module
const descriptor_name = "cpm.json"

var fetched map[string]bool
var built map[string]bool

var inprocess []string

//command line flags
var fetch_flag, verbose_flag *bool

func main() {
	var err error
	fetch_flag = flag.Bool("f", false, "fetch only (no build)")
	verbose_flag = flag.Bool("v", false, "verbose")

	if devroot = os.Getenv("DEV_ROOT"); len(devroot) == 0 {
		log.Fatal("Environment variable  DEV_ROOT not set")
	}
	//make sure DEV_ROOT is terminated with a path separator
	if devroot[len(devroot)-1] != '/' && devroot[len(devroot)-1] != '\\' {
		devroot += "/"
	}
	flag.Usage = func() {
		println(`C/C++ Package Manager
  Usage: cpm [options] [project]
				
  If project is not specified, it is assumed to be the current directory.
  Valid options are:
    -f fetch-only (no build)
    -v verbose
    -h help - prints this message`)
	}

	flag.Parse()

	if flag.NArg() > 0 {
		root_descriptor = devroot + flag.Arg(0) + "/" + descriptor_name
	} else {
		cwd, _ := os.Getwd()
		root_descriptor = cwd + "/" + descriptor_name
	}

	Verboseln("DEV_ROOT = " + devroot)
	os.Mkdir(devroot+"lib", 0755)

	fetched = make(map[string]bool)
	var root PacUnit
	var data []byte
	if data, err = os.ReadFile(root_descriptor); err != nil {
		log.Fatalf("cannot open '%s' file", root_descriptor)
	}

	if err = json.Unmarshal(data, &root); err != nil {
		log.Fatalf("cannot parse %s - %v\n", root_descriptor, err)
	}

	os.Chdir(devroot + root.Name)

	cwd, _ := os.Getwd()
	Verboseln("Changed directory to " + cwd)

	inprocess = make([]string, 0, 10)
	fetch(&root)
	if !*fetch_flag {
		built = make(map[string]bool)
		build(&root)
	}
}

// Fetch a package and all its dependents
func fetch(p *PacUnit) {
	if fetched[p.Name] {
		Verboseln("Package " + p.Name + " has already been configured")
		return
	}
	pacdir := devroot + p.Name
	if os.Chdir(pacdir) != nil {
		if err := os.Mkdir(pacdir, 0666); err != nil {
			log.Fatalf("error %d - cannot create folder %s", err, pacdir)
		}
		clone(p.Git, p.Name)
		os.Chdir(pacdir)
	}

	cwd, _ := os.Getwd()
	Verbosef("Setting up %s in %s \n", p.Name, cwd)

	r, err := git.PlainOpen(".")
	if err != nil {
		log.Fatalf("cannot open repository in %s error %v", cwd, err)
	}

	w, _ := r.Worktree()
	w.Pull(&git.PullOptions{})
	os.Symlink(devroot+"lib", "lib")

	data, err := os.ReadFile(descriptor_name)
	if err != nil {
		Verbosef(" %s\\%s file not found. Assuming no dependencies\n", cwd, descriptor_name)
	} else {
		if err = json.Unmarshal(data, &p); err != nil {
			log.Fatalf("cannot parse %s - %v", descriptor_name, err)
		}
	}
	fetched[p.Name] = true

	if p.Depends != nil {
		//setup all dependent packages
		var d *PacUnit
		for _, d = range p.Depends {
			fetch(d)
		}

		if os.Chdir(pacdir+"/include") != nil {
			os.Mkdir(pacdir+"/include", 0755)
			os.Chdir(pacdir + "/include")
		}

		//create symlinks to dependents
		for _, dep := range p.Depends {
			Verboseln("Creating symlink " + devroot + dep.Name + "/include/" + dep.Name + " --> " + dep.Name)
			os.Symlink(devroot+dep.Name+"/include/"+dep.Name, dep.Name)
		}
	}
}

//Build a packge after having built first its dependents
func build(p *PacUnit) {
	if built[p.Name] {
		Verboseln("Package " + p.Name + " has already been built")
		fmt.Printf("Build map: % v", built)
		return
	}
	for _, w := range inprocess {
		if w == p.Name {
			log.Fatalf("Package %s depends on itself.\n Dependency chain: %v", p.Name, inprocess)
		}
	}

	//keep track of packeges that are in process to avoid dependency cycles
	inprocess = append(inprocess, p.Name)
	pacdir := devroot + p.Name
	os.Chdir(pacdir) //that should be ok. Package has been fetched already
	cwd, _ := os.Getwd()
	Verbosef("Building %s in %s \n", p.Name, cwd)

	//First, build all dependent packages
	if p.Depends != nil {
		var d *PacUnit
		for _, d = range p.Depends {
			build(d)
			os.Chdir(cwd)
		}
	}

	// then build self
	if ret, err := do_build(p.Build); ret != 0 {
		log.Fatalf("Build aborted - %v", err)
	}
	inprocess = inprocess[:len(inprocess)-1]
	built[p.Name] = true
}

/*
	Execute the appropriate build command for a package. If there is a specific
	command for the current OS envirnoment, use that one. Otherwise choose a
	generic one (os set to "any" or "")
*/
func do_build(b []*BuildSet) (int, error) {
	for _, cfg := range b {
		if cfg.Os == runtime.GOOS {
			Verbosef("OS: %s cmd: %s % v\n", cfg.Os, cfg.Cmd, cfg.Args)
			return Run(cfg.Cmd, cfg.Args)
		}
	}
	for _, cfg := range b {
		if cfg.Os == "any" || cfg.Os == "" {
			Verbosef("cmd: %s % v\n", cfg.Cmd, cfg.Args)
			return Run(cfg.Cmd, cfg.Args)
		}
	}
	Verboseln("No build command found!")
	return 0, nil
}

/*
  Executes a program and waits for it to finish.
  Returns exit code and error condition.

  Seems in Windows, the osStartProcess function needs the absolute path of the program.
  The easiest way around it is to run a CMD shell with the program passed as an argument:
    %SystemRoot%\\system32\cmd.exe /c prog args
*/
func Run(prog string, args []string) (int, error) {
	if runtime.GOOS == "windows" {
		if len(args) == 0 {
			args = append(args, "/C")
		} else {
			args = append(args[:1], args[0:]...)
			args[0] = "/C"
		}

		if len(args) == 1 {
			args = append(args, prog)
		} else {
			args = append(args[:2], args[1:]...)
			args[1] = prog
		}
		systemRootDir := os.Getenv("SystemRoot")
		prog = systemRootDir + "\\System32\\cmd.exe"
		//insert "/C" argument for CMD
	}
	var proc *os.Process
	var s *os.ProcessState
	var err error
	var attr os.ProcAttr
	attr.Files = []*os.File{os.Stdin, os.Stdout, os.Stderr}

	if proc, err = os.StartProcess(prog, args, &attr); err != nil {
		return -1, err
	}
	if s, err = proc.Wait(); err != nil {
		return -1, err
	}
	return s.ExitCode(), err
}

func clone(url, dir string) bool {
	fullpath := devroot + dir

	Verbosef("Cloning: %s in %s", url, dir)

	if stat, err := Run("git", []string{"clone", url, fullpath}); err != nil || stat != 0 {
		log.Fatalf("Cloning failed \nStatus %d Error: %v\n", stat, err)
	}
	return true
}

func Verboseln(s string) {
	if *verbose_flag {
		fmt.Println(s)
	}
}

func Verbosef(f string, a ...interface{}) {
	if *verbose_flag {
		fmt.Printf(f, a...)
	}
}
