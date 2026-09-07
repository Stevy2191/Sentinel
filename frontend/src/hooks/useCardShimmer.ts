import { useCallback, useState } from 'react'

interface ShimmerState {
  x: number
  y: number
  show: boolean
}

const IDLE: ShimmerState = { x: 0, y: 0, show: false }

/**
 * useCardShimmer tracks an independent cursor position per card, so a hover
 * highlight follows the pointer on the card being hovered and no other.
 *
 * Positions are stored per id rather than as one shared value: with a single
 * position every card would light up together, which is what makes the effect
 * read as a cheap overlay instead of a material.
 */
export const useCardShimmer = (cardIds: string[]) => {
  const [cardShimmers, setCardShimmers] = useState<Record<string, ShimmerState>>(() => {
    const initial: Record<string, ShimmerState> = {}
    cardIds.forEach((id) => {
      initial[id] = { ...IDLE }
    })
    return initial
  })

  const handleCardMouseMove = useCallback((e: React.MouseEvent, cardId: string) => {
    const rect = e.currentTarget.getBoundingClientRect()
    const x = e.clientX - rect.left
    const y = e.clientY - rect.top
    setCardShimmers((prev) => ({
      ...prev,
      [cardId]: { ...(prev[cardId] ?? IDLE), x, y },
    }))
  }, [])

  const handleCardMouseEnter = useCallback((cardId: string) => {
    setCardShimmers((prev) => ({
      ...prev,
      [cardId]: { ...(prev[cardId] ?? IDLE), show: true },
    }))
  }, [])

  const handleCardMouseLeave = useCallback((cardId: string) => {
    setCardShimmers((prev) => ({
      ...prev,
      [cardId]: { ...(prev[cardId] ?? IDLE), show: false },
    }))
  }, [])

  // Falls back to IDLE for ids that were not in the initial list. Monitor type
  // cards are built from live data, so the set of ids is not always known when
  // the hook is created, and reading through would otherwise throw.
  const isShown = useCallback((cardId: string) => (cardShimmers[cardId] ?? IDLE).show, [cardShimmers])

  const getShimmerStyle = useCallback(
    (cardId: string): React.CSSProperties => {
      const { x, y } = cardShimmers[cardId] ?? IDLE
      return {
        backgroundImage: `radial-gradient(circle 120px at ${x}px ${y}px, rgba(255,255,255,0.12), transparent 70%)`,
      }
    },
    [cardShimmers]
  )

  return {
    cardShimmers,
    isShown,
    handleCardMouseMove,
    handleCardMouseEnter,
    handleCardMouseLeave,
    getShimmerStyle,
  }
}
