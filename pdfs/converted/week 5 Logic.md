LimCK

## WQF7003 Intelligent Computation

## Logic

1

## What is logic?

- The study of reasoning
- Focuses on the relationship among statements as opposed to the content of any particular statement.
- Instead of focusing on the statements, we use symbols to represent them.
- &gt;&gt; Symbolic logic

<!-- image -->

<!-- image -->

## Types of logic

- Propositional logic
- First order logic
- Probabilistic Logic
- Fuzzy Logic
- …

## Statement

- Statement/Proposition : a sentence that is either TRUE or FALSE, but not both.
- -I can run.
- -I cannot run
- -2 &gt; 1
- -1 &gt; 2
- We can use variables (a, b, c, P, Q, R, …) to represent propositions.

## Logical connectives

- NOT ● AND ● OR ● IF … THAN ● IFF

## Negation, ¬

- Negation of p , denoted ¬ p , is the proposition:

not p

- Example:
- p : Bicycles have windscreens
- ¬ p : Bicycles do not have windscreens
- Notations: ¬ p , ~ p , ! p , p ' or p .
- Or, in English, we can use 'It is not the case that p ' to express a negation.

## AND, ⋀

- We can combine propositions with AND.
- Example:
- 'p q'  is  '2+2=4 and birds can swim.' ⋀

```
p: 2+2=4     q: birds can swim.
```

Truth table

| P   | Q   | P Q ⋀   |
|-----|-----|---------|
| T   | T   | T       |
| T   | F   | F       |
| F   | T   | F       |
| F   | F   | F       |

## OR, ⋁

- We can combine propositions with OR.
- Example:
- 'p q'  is  '2+2=4 or birds can swim.' ⋁

```
p: 2+2=4     q: birds can swim.
```

Truth table

| P   | Q   | P Q ⋁   |
|-----|-----|---------|
| T   | T   | T       |
| T   | F   | T       |
| F   | T   | T       |
| F   | F   | F       |

## Conditional Proposition

- The propositions in the form
- if p then q
- are conditional propositions that denote as :

<!-- formula-not-decoded -->

- Example:

If you are in Kuala Lumpur,

- then you are in Malaysia.
- p: you are in KL             (antecedent)
- q: you are in Malaysia   (consequent)

## Truth Table of Conditional Proposition

Given:  If you are in KL, than you are in Malaysia

| p   | q   | p→q   |                                    |
|-----|-----|-------|------------------------------------|
| T   | T   | T     | You are in KL, you are in Malaysia |
| T   | F   | F     |                                    |
| F   | T   | T     |                                    |
| F   | F   | T     |                                    |

## Truth Table of Conditional Proposition

## Given:  If you are in KL, than you are in Malaysia

| p   | q   | p→q   |                                             |
|-----|-----|-------|---------------------------------------------|
| T   | T   | T     | You are in KL, you are in Malaysia          |
| T   | F   | F     | You are in KL, but not in Malaysia? No way! |
| F   | T   | T     |                                             |
| F   | F   | T     |                                             |

## Truth Table of Conditional Proposition

## Given:  If you are in KL, than you are in Malaysia

| p   | q   | p→q   |                                             |
|-----|-----|-------|---------------------------------------------|
| T   | T   | T     | You are in KL, you are in Malaysia          |
| T   | F   | F     | You are in KL, but not in Malaysia? No way! |
| F   | T   | T     | You are in Sabah!                           |
| F   | F   | T     | You are in Sabah!                           |

Do not violate the definition in the given p → q, or, not falsified by the given condition

## Biconditional Proposition

- Bidirectional Propositions - in the form:

p if and only if q

- Example : Integer x is divisible by 2 if and only if x is an even number.
- Denote as :           p ↔ q
- Can be written as p iff q
- Equivalent to (p→q) ⋀ (q→p)

## Truth table

| p   | q   | p→q   | q→p   | p→q ⋀   | p↔q   |
|-----|-----|-------|-------|---------|-------|
| T   | T   | T     | T     | T       | T     |
| T   | F   | F     | T     | F       | F     |
| F   | T   | T     | F     | F       | F     |
| F   | F   | T     | T     | T       | T     |

## Order of Precedence

| Operator   |   Precedence |
|------------|--------------|
|            |            1 |
|            |            2 |
| V          |              |
| →          |            4 |
|            |            5 |

## Logical Equivalent

- Assume A and B are two propositions,
- A and B are 'logically equivalent', or

```
A = … … … B = … …
```

A ↔ B

- iff A and B have the same truth values.
- 2 ways to determine logical equivalent:
- -Truth Table
- -Laws of Logic

## Truth Table

- 2 propositions are logical equivalent if they have same truth values
- Example:  Prove Law of Implication [show that p→q and ~pVq are logically equivalent]

<!-- image -->

Same truth values

- → logical equivalent

## Laws of Logic

| p^T=p pvF=p             | Identitylaws       |
|-------------------------|--------------------|
| pvT=T PAF=F             | Dominationlaws     |
| pVp=p pAp=p             | Idempotentlaws     |
| d =(d-)                 | Double negationlaw |
| pvq=qvp pAq=qAp         | Commutativelaws    |
| (b)d=(bd) (vb)vd=v(bvd) | Associativelaws    |

## Laws of Logic

| pV(q^r)=(pVq)(pVr) p(qVr)=(pq)(pr)   | Distributivelaws   |
|--------------------------------------|--------------------|
| b-^d-=(bvd) b-vd=(b^d)-              | DeMorgan'slaws     |
| pv(p^q)=p p^(pVq)=p                  | Absorptionlaws     |
| pV-p=T PA-p=F                        | Negationlaws       |

<!-- formula-not-decoded -->

Law of Implication

## Example

(p ^q) → (p v q) is a tautology Showthat

<!-- formula-not-decoded -->

<!-- formula-not-decoded -->

<!-- formula-not-decoded -->

<!-- formula-not-decoded -->

<!-- formula-not-decoded -->

<!-- formula-not-decoded -->

Implication law

De Morgan's law

Associative law

Associative law

Negation law

## Deductive Reasoning

## Deductive Reasoning

- Consider the following sequence of propositions.
- a)We are looking for a man either in black or yellow shirt
- b)The man with black shirt is wearing jeans.
- c)The man we are looking for is not wearing jeans
- Assuming that these statements are true, it is reasonable to conclude:

The man we are looking for is wearing yellow shirt

- drawing a conclusion from a sequence of propositions → Deductive Reasoning

## Deductive Reasoning

The following is an argument .

- a)We are looking for a man either in black or yellow shirt
- c)The man we are looking for is not wearing jeans
- b)The man with black shirt is wearing jeans.

-----------------------------------------------------------------

Therefore, the man we are looking for is wearing yellow shirt

(a), (b), (c) are called hypothesis / premises

```
'Therefore, … ….'    is the conclusion
```

## Valid or Invalid

- In a valid argument, we say that the conclusion follows from the hypotheses.
- Note: We are not saying that the conclusion is true; we are only saying that if you grant the hypotheses, you must also grant the conclusion.
- An argument is valid because of its form, not because of its content.

## Rules of Inference

- Rules of inference are methods for deriving conclusions from premises.
- They form a core component of formal logic, providing the structure that makes arguments valid.
- When an argument with true premises adheres to a rule of inference, its conclusion must also be true.

## Rules of Inference

## Modus Ponens

```
p → q p ---------∴ q
```

## Rules of Inference

## Modus Ponens

```
p → q p ---------∴ q
```

Example

If this object is made of copper, it will conduct electricity .

This object is made of copper.

Therefore, it will conduct electricity

## Rules of Inference

## Modus Tollens

```
p → q ~q ---------∴ ~ p
```

## Rules of Inference

## Modus Tollens

```
p → q ~q ---------∴ ~ p
```

Example

If I am hungry I eat a lot I don't eat a lot

So I am not hungry

## Rules of Inference

## Hypothetical Syllogism

```
p → q q → r ---------∴ p → r
```

## Rules of Inference

## Hypothetical Syllogism

<!-- formula-not-decoded -->

## Example

If it rains, we will not have a picnic.

If we don't have a picnic, we won't need a picnic basket.

Therefore, if it rains, we won't need a picnic basket.

## Rules of Inference

## Dilemma

```
p V q p → r q → s ---------∴ r V s
```

## Rules of Inference

## Dilemma

```
p V q p → r q → s ---------∴ r V s
```

Example

Either we take LRT or we drive If we take LRT, we pay the fare If we drive, we pay tolls So, either we pay LRT fare or we pay tolls

## There are some common fallacy

<!-- image -->

## Affirming the consequent

<!-- formula-not-decoded -->

---------

∴ p If am in Thailand, then I am in Asia.

I am in Asia.

Therefore I am in Thailand.

## There are some common fallacy

<!-- image -->

## Denying the antecedent

<!-- formula-not-decoded -->

---------

<!-- formula-not-decoded -->

If am in Thailand, then I am in Asia.

I am not in Thailand.

Therefore I am not in Asia.

- ●

## Example

- Identify the conclusion is valid or not: 'If birds can fly or swim, then they can find food. Birds can fly. Tigers can find food.

Therefore, birds and tigers can find food.'

## First Order Logic / Predicate Logic

## Statements that no one can tell true or false

- x is greater than 10.
- There is an old man who can cure cancer without using any medicine.

x is greater than 10.

Subject     Predicate (refers to a property that the subject

may have)

These statements can be denoted by propositional function, e.g. G(x) or C(x)

## Quantifiers

- Quantification : to create a proposition from a propositional function.
- In English, the words all, some, many, none, and few are used in quantification.
- We focus on 2 types of quantification: universal and existential quantification.

## Universal Quantification

- assert that a property is true for all values of a variable in the domain of discourse .
- -i.e. P(x) is true for all values of x in this domain.
- The domain must always be specified when a universal quantifier is used, since a change of domain may lead to change of truth value or unverifiable.

## Universal Quantification

- The universal quantification of P(x) is the statement:
- 'P(x) for all values of x in the domain.'
- Notation:

<!-- formula-not-decoded -->

- is universal quantifier. Read as 'for all', ∀ 'for every', 'for each', ...

## Existential Quantification

- Proposition that is true if and only if P(x) is true for at least one value of x in the domain.
- The existential quantification of P(x) is the proposition:
- 'There exists an element x in the domain such that P(x).'
- Notation : xP(x) ∃

## Existential Quantification

- is called the existential quantifier, read ∃ as 'there exists,' 'for some,' 'for at least one,' or 'there is.'
- xP(x) is read as: ∃
- 'There is an x such that P(x),'

'There is at least one x such that P(x),'

or

'For some x P(x).'

## Examples

- Let P(x) be the statement 'x + 1 &gt; x' for all real numbers x. Is xP(x) true? \_\_\_\_\_\_\_\_ ∀
- Let Q(x) be the statement 'x &lt; 2' where the domain consists of all real numbers. Is xQ(x) ∀ true? \_\_\_\_\_\_\_\_
- Let Q(x) be the statement 'x &lt; 2' where the domain consists of all real numbers. Is xQ(x) ∃ true? \_\_\_\_\_\_\_\_
- Let R(x) be the statement 'x is the sibling of x' where the domain consist of everyone in the world. Is xR(x) true? ∃ \_\_\_\_\_\_\_\_

## Domains of Quantifiers

- The domains of quantifiers can be expressed in mathematical form.
- Example:

<!-- formula-not-decoded -->

x , x+1&gt;x ∀ ∊ℝ

<!-- formula-not-decoded -->

## Examples of Quantified Statements

- 'If x likes BLACKPINK then he likes K-Pop'
- B(x) → K(x)
- Proposition?
- 'For all people it holds that if the person likes BLACKPINK then she likes K-Pop'

●

- Proposition?

## Examples of Quantified Statements

- 'If x likes BLACKPINK then he likes K-Pop'
- B(x) → K(x)
- Proposition? No
- 'For all people it holds that if the person likes BLACKPINK then she likes K-Pop'
- x(B(x) → K(x)) ∀
- Proposition? Yes

## Examples of Quantified Statements

- 'x is your classmate who has founded Microsoft'
- C(x) /\ M(x)
- Proposition?
- 'There is a person who is your classmate and the founder of Microsoft'

●

- Proposition?

## Examples of Quantified Statements

- 'x is your classmate who has founded Microsoft'
- C(x) /\ M(x)
- Proposition? No
- 'There is a person who is your classmate and the founder of Microsoft'
- x ( C(x) /\ M(x) ) ∃
- Proposition? Yes

## Probabilistic logic

## Probability

- Probability of X happening = (Number of ways X can happen) / (Total number of outcomes)
- Example: Throwing a dice and the outcome is 3 → 1/6
- Example: Toss a coin and get a Head → ½
- Example: Throw dice and toss coin together, and get '3' and 'Head'? \_\_\_\_\_\_

## Probabilistic logic

- combines the capacity of probability theory to handle uncertainty with the formal structure of deductive logic.
- While classical logic deals with 0 and 1, probabilistic logic uses the continuous interval [0,1].
- A value of P(A)=0.7 indicates a 70% degree of likelihood that proposition A is true.

- ●

## Example

- Instead of: If you are tall, you are a good basketball player.
- Probabilistic logic
- If you are tall, you are more likely to be a good basketball player.

```
(75%???)
```

- Instead of:

If you follow this treatment, you will be cured

- Probabilistic logic

If you follow this treatment, there is a chance you will be cured

```
(1%????  50%???   95%???)
```

## Example

## Fuzzy Logic

Harry Potter 165cm

## Fuzzy Logic

- Deal with uncertainty and vagueness
- Example: Who is the shortest tall man?

<!-- image -->

<!-- image -->

Tom Cruise 172cm

Johnny Depp 175cm

<!-- image -->

Daniel Craig 178cm

<!-- image -->

## ● Or, which one is green?

<!-- image -->

## Fuzzy Sets

- Green, Tall Man, etc are all fuzzy sets.
- Each element in a fuzzy set comes with a membership degree in [0,1]
- Membership degrees are defined by membership functions

```
Leonardo DiCaprio : 181cm Johnny Depp : 175cm Harry Potter : 165cm T(Leonardo DiCaprio) = 1.0
```

```
T(Johnny Depp) = 0.75 T(Harry Potter) = 0.25
```

<!-- image -->

## Fuzzy Sets

- T - fuzzy set Tall
- T(x) - fuzzy membership degree for Tall
- Johnny Depp and Harry Potter are elements in fuzzy set Tall
- T(Harry Potter) = 0.25 → Harry Potter's degree in Tall

<!-- image -->

```
Leonardo DiCaprio : 181cm Johnny Depp : 175cm Harry Potter : 165cm T(Leonardo DiCaprio) = 1.0
```

T(Johnny Depp) = 0.75 T(Harry Potter) = 0.25

## Fuzzy rules and fuzzy logic (example)

- If you are tall, you are a good basketball player.
- T(x) : what is the membership degree of x in Tall
- B(y) = what is the membership degree of y as a good basketball player?
- What is the relationship between T and B?

## Any question?