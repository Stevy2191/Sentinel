export interface PasswordFeedback {
  hasLength: boolean
  hasUppercase: boolean
  hasNumber: boolean
  hasSpecial: boolean
}

export interface PasswordStrength {
  score: number // 0-4
  feedback: PasswordFeedback
  isStrong: boolean
}

// Mirrors the backend policy: >=12 chars, uppercase, number, special (!@#$%^&*).
export function validatePassword(password: string): PasswordStrength {
  const feedback: PasswordFeedback = {
    hasLength: password.length >= 12,
    hasUppercase: /[A-Z]/.test(password),
    hasNumber: /[0-9]/.test(password),
    hasSpecial: /[!@#$%^&*]/.test(password),
  }
  const score =
    (feedback.hasLength ? 1 : 0) +
    (feedback.hasUppercase ? 1 : 0) +
    (feedback.hasNumber ? 1 : 0) +
    (feedback.hasSpecial ? 1 : 0)
  return { score, feedback, isStrong: score === 4 }
}
