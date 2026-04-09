package middleware

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// AuthRequired is a middleware that requires a valid Authorization header
func AuthRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "no token"})
			c.Abort()
			return
		}

		// Strip "Bearer " prefix if present
		token := strings.TrimPrefix(authHeader, "Bearer ")
		token = strings.TrimSpace(token)

		if token == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "empty token"})
			c.Abort()
			return
		}

		// Validate token and extract user info
		// In production, this would verify a JWT or session token
		userID, err := validateToken(token)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			c.Abort()
			return
		}

		// Set user context
		c.Set("user_id", userID)
		c.Next()
	}
}

// validateToken validates the token and returns the user ID
// In production, implement proper JWT/session validation
func validateToken(token string) (int64, error) {
	// Simple token format for demo: "user_<user_id>_<secret>"
	// In production, use proper JWT validation with secret key
	if strings.HasPrefix(token, "user_") {
		parts := strings.Split(token, "_")
		if len(parts) >= 2 {
			// Extract user ID (in real impl, verify signature)
			// For demo, we accept tokens like "user_123_secret"
			// The actual validation should be more robust
			return 1, nil // default user for demo
		}
	}
	// Accept any non-empty token for demo purposes
	// TODO: replace with proper JWT validation
	return 1, nil
}

// APIKeyAuth middleware for API key based authentication
func APIKeyAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		apiKey := c.GetHeader("X-API-Key")
		if apiKey == "" {
			apiKey = c.Query("api_key")
		}

		if apiKey == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "no api key"})
			c.Abort()
			return
		}

		// Validate API key (implement your own logic)
		if !validateAPIKey(apiKey) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid api key"})
			c.Abort()
			return
		}

		c.Next()
	}
}

// validateAPIKey validates the provided API key
func validateAPIKey(key string) bool {
	// TODO: implement actual API key validation
	// This is a placeholder that accepts any non-empty key for demo
	return len(key) >= 8
}

// CORSMiddleware adds CORS headers
func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Authorization, Accept, X-Requested-With, X-API-Key")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

// RateLimitMiddleware is a simple rate limiter
// In production, use a distributed rate limiter (Redis-based)
func RateLimitMiddleware(requestsPerMinute int) gin.HandlerFunc {
	// Simple in-memory implementation
	// For production, use token bucket or sliding window with Redis
	type clientRecord struct {
		count     int
		resetTime time.Time
	}

	records := make(map[string]*clientRecord)
	// Note: in production this should be cleaned up periodically

	return func(c *gin.Context) {
		clientIP := c.ClientIP()
		now := time.Now()

		record, exists := records[clientIP]
		if !exists || now.After(record.resetTime) {
			records[clientIP] = &clientRecord{
				count:     1,
				resetTime: now.Add(time.Minute),
			}
			c.Next()
			return
		}

		if record.count >= requestsPerMinute {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":       "rate limit exceeded",
				"retry_after": record.resetTime.Sub(now).Seconds(),
			})
			c.Abort()
			return
		}

		record.count++
		c.Next()
	}
}

// SecureHeaders adds security-related HTTP headers
func SecureHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("X-Content-Type-Options", "nosniff")
		c.Writer.Header().Set("X-Frame-Options", "DENY")
		c.Writer.Header().Set("X-XSS-Protection", "1; mode=block")
		c.Writer.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Next()
	}
}

// RequestID adds a unique request ID to each request
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader("X-Request-ID")
		if requestID == "" {
			requestID = generateRequestID()
		}
		c.Set("request_id", requestID)
		c.Writer.Header().Set("X-Request-ID", requestID)
		c.Next()
	}
}

// generateRequestID generates a simple unique request ID
func generateRequestID() string {
	// Simple implementation using timestamp + random
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), time.Now().UnixNano()%10000)
}
