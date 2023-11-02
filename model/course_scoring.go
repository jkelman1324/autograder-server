package model

import (
    "fmt"

    "github.com/rs/zerolog/log"
)

func (this *Course) FullScoringAndUpload(dryRun bool) error {
    assignments := this.GetSortedAssignments();

    log.Debug().Str("course", this.ID).Bool("dry-run", dryRun).Msg("Beginning full scoring for course.");

    for i, assignment := range assignments {
        if (assignment.GetLMSID() == "") {
            log.Warn().Str("course", this.ID).Str("assignment", assignment.GetID()).Msg("Assignment has no LMS id, skipping scoring.");
            continue;
        }

        log.Debug().Str("course", this.ID).Str("assignment", assignment.GetID()).Int("index", i).Bool("dry-run", dryRun).
                Msg("Scoring course assignment.");

        err := assignment.FullScoringAndUpload(dryRun);
        if (err != nil) {
            return fmt.Errorf("Failed to grade assignment '%s' for course '%s': '%w'.", this.ID, assignment.GetID(), err);
        }
    }

    log.Debug().Str("course", this.ID).Bool("dry-run", dryRun).Msg("Finished full scoring for course.");

    return nil;
}
