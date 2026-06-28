# Numerical Methods

## Secant Method

The secant method is a root-finding algorithm that uses a succession of roots of secant lines to approximate a root of a function.

### Formula
x_{n+1} = x_n - f(x_n) * (x_n - x_{n-1}) / (f(x_n) - f(x_{n-1}))

### Algorithm
1. Choose two initial guesses x₀ and x₁
2. Compute x₂ using the secant formula
3. If |f(x₂)| < tolerance, stop
4. Otherwise, update x₀ = x₁, x₁ = x₂ and repeat

### Properties
- Converges faster than bisection but slower than Newton's method
- Does not require computing derivatives
- May fail if f(x_n) ≈ f(x_{n-1}) (near-zero denominator)

## Newton-Raphson Method

### Formula
x_{n+1} = x_n - f(x_n) / f'(x_n)

### Properties
- Quadratic convergence near the root
- Requires derivative computation
- May diverge if initial guess is poor

## Bisection Method

### Algorithm
1. Choose interval [a, b] where f(a) · f(b) < 0
2. Compute midpoint c = (a + b) / 2
3. If f(c) = 0, root found
4. Otherwise narrow interval to [a, c] or [c, b] based on sign change
