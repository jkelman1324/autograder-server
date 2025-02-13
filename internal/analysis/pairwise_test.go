package analysis

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/edulinq/autograder/internal/analysis/jplag"
	"github.com/edulinq/autograder/internal/db"
	"github.com/edulinq/autograder/internal/docker"
	"github.com/edulinq/autograder/internal/model"
	"github.com/edulinq/autograder/internal/stats"
	"github.com/edulinq/autograder/internal/timestamp"
	"github.com/edulinq/autograder/internal/util"
)

func TestPairwiseAnalysisFake(test *testing.T) {
	db.ResetForTesting()
	defer db.ResetForTesting()

	assignment := db.MustGetTestAssignment()

	ids := []string{
		"course101::hw0::course-student@test.edulinq.org::1697406256",
		"course101::hw0::course-student@test.edulinq.org::1697406265",
		"course101::hw0::course-student@test.edulinq.org::1697406272",
	}

	expected := []*model.PairwiseAnalysis{
		&model.PairwiseAnalysis{
			Options:           assignment.AnalysisOptions,
			AnalysisTimestamp: timestamp.Zero(),
			SubmissionIDs: model.NewPairwiseKey(
				"course101::hw0::course-student@test.edulinq.org::1697406256",
				"course101::hw0::course-student@test.edulinq.org::1697406265",
			),
			Similarities: map[string][]*model.FileSimilarity{
				"submission.py": []*model.FileSimilarity{
					&model.FileSimilarity{
						Filename: "submission.py",
						Tool:     "fake",
						Version:  "0.0.1",
						Score:    0.13,
					},
				},
			},
			UnmatchedFiles: [][2]string{},
			SkippedFiles:   []string{},
			MeanSimilarities: map[string]float64{
				"submission.py": 0.13,
			},
			TotalMeanSimilarity: 0.13,
		},
		&model.PairwiseAnalysis{
			Options:           assignment.AnalysisOptions,
			AnalysisTimestamp: timestamp.Zero(),
			SubmissionIDs: model.NewPairwiseKey(
				"course101::hw0::course-student@test.edulinq.org::1697406256",
				"course101::hw0::course-student@test.edulinq.org::1697406272",
			),
			Similarities: map[string][]*model.FileSimilarity{
				"submission.py": []*model.FileSimilarity{
					&model.FileSimilarity{
						Filename: "submission.py",
						Tool:     "fake",
						Version:  "0.0.1",
						Score:    0.13,
					},
				},
			},
			UnmatchedFiles: [][2]string{},
			SkippedFiles:   []string{},
			MeanSimilarities: map[string]float64{
				"submission.py": 0.13,
			},
			TotalMeanSimilarity: 0.13,
		},
		&model.PairwiseAnalysis{
			Options:           assignment.AnalysisOptions,
			AnalysisTimestamp: timestamp.Zero(),
			SubmissionIDs: model.NewPairwiseKey(
				"course101::hw0::course-student@test.edulinq.org::1697406265",
				"course101::hw0::course-student@test.edulinq.org::1697406272",
			),
			Similarities: map[string][]*model.FileSimilarity{
				"submission.py": []*model.FileSimilarity{
					&model.FileSimilarity{
						Filename: "submission.py",
						Tool:     "fake",
						Version:  "0.0.1",
						Score:    0.13,
					},
				},
			},
			UnmatchedFiles: [][2]string{},
			SkippedFiles:   []string{},
			MeanSimilarities: map[string]float64{
				"submission.py": 0.13,
			},
			TotalMeanSimilarity: 0.13,
		},
	}

	testPairwise(test, ids, expected, 0)

	// Test again, which should pull from the cache.
	testPairwise(test, ids, expected, len(expected))

	// After both runs, there should be exactly one stat record (since the second one was cached).
	results, err := db.GetCourseMetrics(stats.CourseMetricQuery{CourseID: "course101"})
	if err != nil {
		test.Fatalf("Failed to do stats query: '%v'.", err)
	}

	expectedStats := []*stats.CourseMetric{
		&stats.CourseMetric{
			BaseMetric: stats.BaseMetric{
				Timestamp: timestamp.Zero(),
				Attributes: map[string]any{
					stats.ATTRIBUTE_KEY_ANALYSIS: "pairwise",
				},
			},
			Type:         stats.CourseMetricTypeCodeAnalysisTime,
			CourseID:     "course101",
			AssignmentID: "hw0",
			UserEmail:    "server-admin@test.edulinq.org",
			Value:        3, // 1 for each run of the fake engine.
		},
	}

	// Zero out the query results.
	for _, result := range results {
		result.Timestamp = timestamp.Zero()
	}

	if !reflect.DeepEqual(expectedStats, results) {
		test.Fatalf("Stat results not as expected. Expected: '%s', Actual: '%s'.",
			util.MustToJSONIndent(expectedStats), util.MustToJSONIndent(results))
	}
}

func testPairwise(test *testing.T, ids []string, expected []*model.PairwiseAnalysis, expectedInitialCacheCount int) {
	// Check for records in the DB.
	queryKeys := make([]model.PairwiseKey, 0, len(expected))
	for _, analysis := range expected {
		queryKeys = append(queryKeys, analysis.SubmissionIDs)
	}

	queryResult, err := db.GetPairwiseAnalysis(queryKeys)
	if err != nil {
		test.Fatalf("Failed to do initial query for cached anslysis: '%v'.", err)
	}

	if len(queryResult) != expectedInitialCacheCount {
		test.Fatalf("Number of (pre) cached anslysis results not as expected. Expected: %d, Actual: %d.", expectedInitialCacheCount, len(queryResult))
	}

	results, pendingCount, err := PairwiseAnalysis(ids, true, "server-admin@test.edulinq.org")
	if err != nil {
		test.Fatalf("Failed to do pairwise analysis: '%v'.", err)
	}

	if pendingCount != 0 {
		test.Fatalf("Found %d pending results, when 0 were expected.", pendingCount)
	}

	// Zero out the timestamps.
	for _, result := range results {
		result.AnalysisTimestamp = timestamp.Zero()
	}

	if !reflect.DeepEqual(expected, results) {
		test.Fatalf("Results not as expected. Expected: '%s', Actual: '%s'.",
			util.MustToJSONIndent(expected), util.MustToJSONIndent(results))
	}

	queryResult, err = db.GetPairwiseAnalysis(queryKeys)
	if err != nil {
		test.Fatalf("Failed to do query for cached anslysis: '%v'.", err)
	}

	if len(queryResult) != len(expected) {
		test.Fatalf("Number of (post) cached anslysis results not as expected. Expected: %d, Actual: %d.", len(expected), len(queryResult))
	}
}

func TestPairwiseWithPythonNotebook(test *testing.T) {
	db.ResetForTesting()
	defer db.ResetForTesting()

	tempDir := util.MustMkDirTemp("test-analysis-pairwise-")
	defer util.RemoveDirent(tempDir)

	err := util.CopyDir(filepath.Join(util.RootDirForTesting(), "testdata", "files", "python_notebook"), tempDir, true)
	if err != nil {
		test.Fatalf("Failed to prep temp dir: '%v'.", err)
	}

	paths := [2]string{
		filepath.Join(tempDir, "ipynb"),
		filepath.Join(tempDir, "py"),
	}

	sims, unmatches, _, _, err := computeFileSims(paths, nil, nil)
	if err != nil {
		test.Fatalf("Failed to compute file similarity: '%v'.", err)
	}

	if len(unmatches) != 0 {
		test.Fatalf("Unexpected number of unmatches. Expected: 0, Actual: %d, Unmatches: '%s'.", len(unmatches), util.MustToJSONIndent(unmatches))
	}

	expected := map[string][]*model.FileSimilarity{
		"submission.py": []*model.FileSimilarity{
			&model.FileSimilarity{
				Filename:         "submission.py",
				OriginalFilename: "submission.ipynb",
				Tool:             "fake",
				Version:          "0.0.1",
				Score:            0.13,
			},
		},
	}

	if !reflect.DeepEqual(expected, sims) {
		test.Fatalf("Results not as expected. Expected: '%s', Actual: '%s'.", util.MustToJSONIndent(expected), util.MustToJSONIndent(sims))
	}
}

// Ensure that the default engines run.
// Full output checking will be left to the fake engine.
func TestPairwiseAnalysisDefaultEnginesBase(test *testing.T) {
	docker.EnsureOrSkipForTest(test)

	forceDefaultEnginesForTesting = true
	defer func() {
		forceDefaultEnginesForTesting = false
	}()

	defaultSimilarityEngines[1].(*jplag.JPlagEngine).MinTokens = 5
	defer func() {
		defaultSimilarityEngines[1].(*jplag.JPlagEngine).MinTokens = jplag.DEFAULT_MIN_TOKENS
	}()

	ids := []string{
		"course101::hw0::course-student@test.edulinq.org::1697406256",
		"course101::hw0::course-student@test.edulinq.org::1697406272",
	}

	results, pendingCount, err := PairwiseAnalysis(ids, true, "server-admin@test.edulinq.org")
	if err != nil {
		test.Fatalf("Failed to do pairwise analysis: '%v'.", err)
	}

	if pendingCount != 0 {
		test.Fatalf("Found %d pending results, when 0 were expected.", pendingCount)
	}

	if len(results) != 1 {
		test.Fatalf("Number of results not as expected. Expected: %d, Actual: %d.", 1, len(results))
	}
}

// A test for special files that seem to cause trouble with the engines.
func TestPairwiseAnalysisDefaultEnginesSpecificFiles(test *testing.T) {
	docker.EnsureOrSkipForTest(test)

	// Override a setting for JPlag for testing.
	defaultSimilarityEngines[1].(*jplag.JPlagEngine).MinTokens = 5
	defer func() {
		defaultSimilarityEngines[1].(*jplag.JPlagEngine).MinTokens = jplag.DEFAULT_MIN_TOKENS
	}()

	testPaths := []string{
		filepath.Join(util.RootDirForTesting(), "testdata", "files", "sim_engine", "config.json"),
	}

	for _, path := range testPaths {
		for _, engine := range defaultSimilarityEngines {
			sim, _, err := engine.ComputeFileSimilarity([2]string{path, path}, "")
			if err != nil {
				test.Errorf("Engine '%s' failed to compute similarity on '%s': '%v'.",
					engine.GetName(), path, err)
				continue
			}

			expected := 1.0
			if !util.IsClose(expected, sim.Score) {
				test.Errorf("Engine '%s' got an unexpected score on self-similarity with '%s'. Expected: %f, Actual: %f.",
					engine.GetName(), path, expected, sim.Score)
				continue
			}
		}
	}
}

func TestPairwiseAnalysisIncludeExclude(test *testing.T) {
	db.ResetForTesting()
	defer db.ResetForTesting()

	testCases := []struct {
		options       *model.AnalysisOptions
		expectedCount int
	}{
		{
			nil,
			1,
		},
		{
			&model.AnalysisOptions{
				IncludePatterns: []string{
					`\.c$`,
				},
			},
			0,
		},
		{
			&model.AnalysisOptions{
				ExcludePatterns: []string{
					`\.c$`,
				},
			},
			1,
		},
		{
			&model.AnalysisOptions{
				ExcludePatterns: []string{
					`\.py$`,
				},
			},
			0,
		},
	}

	assignment := db.MustGetTestAssignment()
	ids := []string{
		"course101::hw0::course-student@test.edulinq.org::1697406256",
		"course101::hw0::course-student@test.edulinq.org::1697406265",
	}
	relpath := "submission.py"
	baseCount := 1

	for i, testCase := range testCases {
		db.ResetForTesting()

		if testCase.options != nil {
			err := testCase.options.Validate()
			if err != nil {
				test.Errorf("Case %d: Options is invalid: '%v'.", i, err)
				continue
			}
		}

		assignment.AnalysisOptions = testCase.options
		db.MustSaveAssignment(assignment)

		results, pendingCount, err := PairwiseAnalysis(ids, true, "server-admin@test.edulinq.org")
		if err != nil {
			test.Errorf("Case %d: Failed to perform analysis: '%v'.", i, err)
			continue
		}

		if pendingCount != 0 {
			test.Errorf("Case %d: Found %d pending results, when 0 were expected.", i, pendingCount)
			continue
		}

		if len(results) != 1 {
			test.Errorf("Case %d: Found %d results, when 1 was expected.", i, len(results))
			continue
		}

		if testCase.expectedCount != len(results[0].Similarities) {
			test.Errorf("Case %d: Unexpected number of result similarities. Expected: %d, Actual: %d.",
				i, testCase.expectedCount, len(results[0].Similarities))
			continue
		}

		if (baseCount - testCase.expectedCount) != len(results[0].SkippedFiles) {
			test.Errorf("Case %d: Unexpected number of skipped files. Expected: %d, Actual: %d.",
				i, (baseCount - testCase.expectedCount), len(results[0].SkippedFiles))
			continue
		}

		if testCase.expectedCount == 0 {
			if relpath != results[0].SkippedFiles[0] {
				test.Errorf("Case %d: Unexpected skipped file. Expected: '%s', Actual: '%s'.",
					i, relpath, results[0].SkippedFiles[0])
			}
		}
	}
}
