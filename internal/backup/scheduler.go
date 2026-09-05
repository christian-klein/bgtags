package backup

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/christian-klein/bgtags/internal/database"
)

type Scheduler struct {
	db         *database.DB
	backupDir  string
	backupTime string
	retention  int
	cancel     context.CancelFunc
}

func NewScheduler(db *database.DB, backupDir, backupTime string, retention int) *Scheduler {
	return &Scheduler{
		db:         db,
		backupDir:  backupDir,
		backupTime: backupTime,
		retention:  retention,
	}
}

func (s *Scheduler) Start(ctx context.Context) {
	subCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	go s.run(subCtx)
}

func (s *Scheduler) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *Scheduler) run(ctx context.Context) {
	log.Printf("[bgtags] Backup scheduler active. Scheduled daily at %s (Retention: %d, Dir: %s)",
		s.backupTime, s.retention, s.backupDir)

	for {
		delay, err := NextRunDuration(s.backupTime, time.Now())
		if err != nil {
			log.Printf("[bgtags] Backup scheduler time parsing error: %v. Defaulting to 24h.", err)
			delay = 24 * time.Hour
		}

		log.Printf("[bgtags] Next automated backup scheduled in %v", delay.Round(time.Minute))

		select {
		case <-ctx.Done():
			log.Printf("[bgtags] Backup scheduler stopping...")
			return
		case <-time.After(delay):
			log.Printf("[bgtags] Triggering scheduled daily backup...")
			meta, err := CreateBackup(s.db, s.backupDir, s.retention)
			if err != nil {
				log.Printf("[bgtags] Scheduled backup failed: %v", err)
			} else {
				log.Printf("[bgtags] Scheduled backup succeeded: %s (%s, %d games)",
					meta.FileName, FormatFileSize(meta.SizeBytes), meta.GameCount)
			}
		}
	}
}

func NextRunDuration(timeStr string, now time.Time) (time.Duration, error) {
	parts := strings.Split(strings.TrimSpace(timeStr), ":")
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid time format '%s', expected HH:MM", timeStr)
	}

	hour, err := strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return 0, fmt.Errorf("invalid hour in '%s'", timeStr)
	}

	minute, err := strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return 0, fmt.Errorf("invalid minute in '%s'", timeStr)
	}

	target := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
	if !target.After(now) {
		target = target.Add(24 * time.Hour)
	}

	return target.Sub(now), nil
}
