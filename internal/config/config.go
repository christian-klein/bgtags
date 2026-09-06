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

	// OIDC & RBAC Configuration
	OIDCEnabled      bool
	OIDCIssuerURL    string
	OIDCClientID     string
	OIDCClientSecret string
	OIDCRedirectURL  string
	OIDCAdminGroup   string
	OIDCUsersGroup   string
	SessionSecret    string
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

		// OIDC & RBAC
		OIDCEnabled:      getEnvBool("OIDC_ENABLED", false),
		OIDCIssuerURL:    getEnv("OIDC_ISSUER_URL", ""),
		OIDCClientID:     getEnv("OIDC_CLIENT_ID", ""),
		OIDCClientSecret: getEnv("OIDC_CLIENT_SECRET", ""),
		OIDCRedirectURL:  getEnv("OIDC_REDIRECT_URL", ""),
		OIDCAdminGroup:   getEnv("OIDC_ADMIN_GROUP", "bgtags-admins"),
		OIDCUsersGroup:   getEnv("OIDC_USERS_GROUP", ""),
		SessionSecret:    getEnv("SESSION_SECRET", "bgtags-default-session-secret-key-32b"),
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
