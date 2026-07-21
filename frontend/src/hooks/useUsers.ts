import { useEffect, useState } from 'react'
import api from '@/services/api'
import type { ApiResponse } from '@/types'

export interface UserSummary {
  id: string
  username: string
  email: string
}

/**
 * useUsers loads all users into an id->user map, used to resolve a monitor's
 * owner_id to a username. Any authenticated user may call GET /users.
 */
export function useUsers() {
  const [users, setUsers] = useState<UserSummary[]>([])
  const [usersById, setUsersById] = useState<Record<string, UserSummary>>({})

  useEffect(() => {
    let active = true
    api
      .get<ApiResponse<UserSummary[]>>('/users')
      .then((res) => {
        if (!active) return
        const list = res.data.data ?? []
        setUsers(list)
        const map: Record<string, UserSummary> = {}
        for (const u of list) map[u.id] = u
        setUsersById(map)
      })
      .catch(() => {
        /* non-fatal: usernames just won't resolve */
      })
    return () => {
      active = false
    }
  }, [])

  const usernameFor = (id: string | null | undefined): string | undefined =>
    id ? usersById[id]?.username : undefined

  return { users, usersById, usernameFor }
}
