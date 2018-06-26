package gitbase

import (
	"fmt"
	"time"

	"gopkg.in/src-d/go-log.v1"
	"gopkg.in/src-d/regression-core.v0"
)

type gitbaseResults [][]*Result
type versionResults map[string]gitbaseResults

// Test holds the information about a gitbase test.
type Test struct {
	config    regression.Config
	repos     *regression.Repositories
	testRepos string
	gitbase   map[string]*regression.Binary
	results   versionResults
	queries   []Query
}

// NewTest creates a new Test struct.
func NewTest(config regression.Config) (*Test, error) {
	repos, err := regression.NewRepositories(config)
	if err != nil {
		return nil, err
	}

	return &Test{
		config:  config,
		repos:   repos,
		queries: DefaultQueries,
	}, nil
}

// Prepare downloads repos and binaries needed for the test.
func (t *Test) Prepare() error {
	err := t.prepareRepos()
	if err != nil {
		return err
	}

	err = t.prepareGitbase()
	return err
}

// Run executes the tests.
func (t *Test) Run() error {
	results := make(versionResults)

	for _, version := range t.config.Versions {
		_, ok := results[version]
		if !ok {
			results[version] = make(gitbaseResults, len(t.queries))
		}

		gitbase, ok := t.gitbase[version]
		if !ok {
			panic("gitbase not initialized. Was Prepare called?")
		}

		l := log.With(log.Fields{"version": version})

		l.Infof("Running version tests")

		times := t.config.Repeat
		if times < 1 {
			times = 1
		}

		for s, query := range t.queries {
			results[version][s] = make([]*Result, times)
			for i := 0; i < times; i++ {
				// TODO: do not stop on errors

				l.New(log.Fields{"query": query.Name}).Infof("Running query")

				result, err := t.runTest(gitbase, t.testRepos, query)
				results[version][s][i] = result

				if err != nil {
					return err
				}
			}
		}
	}

	t.results = results

	return nil
}

// GetResults prints test results and returns if the tests passed.
func (t *Test) GetResults() bool {
	if len(t.config.Versions) < 2 {
		panic("there should be at least two versions")
	}

	versions := t.config.Versions
	ok := true
	for i, version := range versions[0 : len(versions)-1] {
		fmt.Printf("#### Comparing %s - %s ####\n", version, versions[i+1])
		a := t.results[versions[i]]
		b := t.results[versions[i+1]]

		for i, query := range t.queries {
			fmt.Printf("## Query %s ##\n", query.Name)

			// TODO: add more options like discard the first run, do the media, etc

			queryA, queryB := getResultsSmaller(a[i], b[i])

			c := queryA.ComparePrint(queryB, 10.0)
			if !c {
				ok = false
			}
		}
	}

	return ok
}

func (t *Test) runTest(
	gitbase *regression.Binary,
	repos string,
	query Query,
) (*Result, error) {
	log.Infof("Executing gitbase test")

	server := NewServer(gitbase.Path, repos)
	err := server.Start()
	if err != nil {
		log.With(log.Fields{
			"repos":   repos,
			"gitbase": gitbase.Path,
		}).Errorf(err, "Could not execute gitbase")
		return nil, err
	}

	queries := NewSQLTest(server.URL(), query)
	err = queries.Connect()

	if err != nil {
		return nil, err
	}

	start := time.Now()

	rows, err := queries.Execute()
	if err != nil {
		return nil, err
	}

	wall := time.Since(start)

	queries.Disconnect()
	server.Stop()

	rusage := server.Rusage()

	log.With(log.Fields{
		"wall":   wall,
		"memory": rusage.Maxrss,
	}).Infof("Finished queries")

	result := &regression.Result{
		Wtime:  wall,
		Stime:  time.Duration(rusage.Stime.Nano()),
		Utime:  time.Duration(rusage.Utime.Nano()),
		Memory: rusage.Maxrss,
	}

	r := &Result{
		Result: result,
		Rows:   rows,
	}

	return r, nil
}

func (t *Test) prepareRepos() error {
	log.Infof("Downloading repositories")
	err := t.repos.Download()
	if err != nil {
		return err
	}

	t.testRepos, err = t.repos.LinksDir()
	return err
}

func (t *Test) prepareGitbase() error {
	log.Infof("Preparing gitbase binaries")
	releases := regression.NewReleases("src-d", "gitbase", t.config.GitHubToken)

	t.gitbase = make(map[string]*regression.Binary, len(t.config.Versions))
	for _, version := range t.config.Versions {
		b := NewGitbase(t.config, version, releases)
		err := b.Download()
		if err != nil {
			return err
		}

		t.gitbase[version] = b
	}

	return nil
}

// Get the runs with lower wall time
func getResultsSmaller(
	a []*Result,
	b []*Result,
) (*Result, *Result) {
	queryA := a[0]
	queryB := b[0]
	for i := 1; i < len(a); i++ {
		if a[i].Wtime < queryA.Wtime {
			queryA = a[i]
		}

		if b[i].Wtime < queryB.Wtime {
			queryB = b[i]
		}
	}

	return queryA, queryB
}
