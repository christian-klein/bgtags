package config

import (
	"os"
	"strconv"
)

type Config struct {
	Port            string
	BaseURL         string
	DBPath          string
	DataDir         string
	StaticDir       string
	BackupEnabled   bool
	BackupDir       string
	BackupTime      string
	BackupRetention int
}

func Load() *Config {
	return &Config{
		Port:            getEnv("PORT", "8081"),
		BaseURL:         getEnv("BASE_URL", ""),
		DBPath:          getEnv("DB_PATH", "data/bgtags.db"),
		DataDir:         getEnv("DATA_DIR", "data"),
		StaticDir:       getEnv("STATIC_DIR", "static"),
		BackupEnabled:   getEnvBool("BACKUP_ENABLED", true),
		BackupDir:       getEnv("BACKUP_DIR", "backups/bgtags"),
		BackupTime:      getEnv("BACKUP_TIME", "00:00"),
		BackupRetention: getEnvInt("BACKUP_RETENTION", 30),
	}
}

func getEnv(key, defaultVal string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return defaultVal
}

func getEnvBool(key string, defaultVal bool) bool {
	if val, ok := os.LookupEnv(key); ok {
		b, err := strconv.ParseBool(val)
		if err == nil {
			return b
		}
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if val, ok := os.LookupEnv(key); ok {
		i, err := strconv.Atoi(val)
		if err == nil {
			return i
		}
	}
	return defaultVal
}
