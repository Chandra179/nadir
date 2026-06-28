# Linear Algebra Fundamentals

## Vectors

A vector v in n-dimensional space is an ordered n-tuple of numbers:
v = (v₁, v₂, ..., vₙ)

### Vector Operations
- Addition: (u + v)ᵢ = uᵢ + vᵢ
- Scalar multiplication: (c · v)ᵢ = c · vᵢ
- Dot product: u · v = Σ uᵢ · vᵢ

## Matrices

An m × n matrix has m rows and n columns.

### Matrix Multiplication
If A is m×n and B is n×p, then C = AB is m×p where:
C_{ij} = Σ_{k=1}^{n} A_{ik} · B_{kj}

### Special Matrices
- Identity matrix I: ones on diagonal, zeros elsewhere
- Diagonal matrix: non-zero only on diagonal
- Symmetric matrix: A = A^T
- Orthogonal matrix: A^T · A = I

## Eigenvalues and Eigenvectors

A vector v ≠ 0 is an eigenvector of A if:
A · v = λ · v

where λ is the corresponding eigenvalue.

### Finding Eigenvalues
Solve det(A - λI) = 0 (characteristic polynomial)

### Applications
- Principal Component Analysis (PCA)
- PageRank algorithm
- Quantum mechanics
- Vibration analysis
