1. When implementing middleware within a service (in-process), you typically focus on logic that requires application context or needs to be highly performant without extra network hops.

2. Checks if the user has specific permissions for a specific resource (e.g., "Can User X edit Document Y?").

3. Capturing the specific internal state of a request, including the function names or database execution times, to help with debugging.

4. Ensuring the JSON payload matches your specific Go structs before it hits the handler. Use Declarative Validation, Instead of writing logic, you use Struct Tags to define rules. Example using gin:

```go
func Validate[T any](c *gin.Context) (T, bool) {
    var req T
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return req, false
    }
    return req, true
}

// Usage in your service:
func CreateOrderHandler(c *gin.Context) {
    req, ok := Validate[OrderRequest](c)
    if !ok { return }
    
    // Proceed with business logic
}
```

5. catches internal code crashes (panics) and returns a clean 500 Internal Server Error instead of crashing the entire process.