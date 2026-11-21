import {
  IconBook,
  IconCategory,
  IconCoin,
  IconFolders,
  IconHistory,
  IconLayoutDashboard,
  IconMessageChatbot,
  IconSettings,
  IconUsers,
  IconUsersGroup,
} from '@tabler/icons-react'

import type { NavMainItem, SidebarMenuData } from '@/types/menu'

import { ROLES, type RoleName } from '@/lib/constants/auth'

const NAV_DASHBOARD: NavMainItem = {
  title: 'Dashboard',
  url: '/dashboard',
  icon: IconLayoutDashboard,
  isActive: true,
  items: [],
}

const NAV_MASTER: NavMainItem = {
  title: 'Master',
  url: '#',
  icon: IconFolders,
  items: [{ title: 'Categories', url: '/master/categories', icon: IconCategory }],
}

const NAV_CHAT: NavMainItem = {
  title: 'Chat AI',
  url: '/chat',
  icon: IconMessageChatbot,
  items: [],
}

const NAV_KNOWLEDGE: NavMainItem = {
  title: 'Basis Pengetahuan',
  url: '/knowledge',
  icon: IconBook,
  items: [],
}

const NAV_TENANTS: NavMainItem = {
  title: 'Tenant',
  url: '/tenants',
  icon: IconUsersGroup,
  items: [],
}
const NAV_LLM: NavMainItem = {
  title: 'Konfigurasi AI',
  url: '/platform/llm',
  icon: IconSettings,
  items: [],
}
const NAV_USERS: NavMainItem = {
  title: 'Pengguna',
  url: '/platform/users',
  icon: IconUsers,
  items: [],
}
const NAV_TOKENS: NavMainItem = {
  title: 'Alokasi Token',
  url: '/platform/tokens',
  icon: IconCoin,
  items: [],
}
const NAV_AUDIT: NavMainItem = {
  title: 'Audit Trail',
  url: '/platform/audit',
  icon: IconHistory,
  items: [],
}

/** Menus by role. Only routes that actually exist are listed. */
const MENUS: Record<RoleName, NavMainItem[]> = {
  [ROLES.SUPER_USER]: [
    NAV_DASHBOARD,
    NAV_CHAT,
    NAV_KNOWLEDGE,
    NAV_TENANTS,
    NAV_USERS,
    NAV_LLM,
    NAV_TOKENS,
    NAV_AUDIT,
    NAV_MASTER,
  ],
  [ROLES.USER]: [NAV_DASHBOARD, NAV_CHAT, NAV_KNOWLEDGE, NAV_TENANTS],
}

const SIDEBAR_MENU: SidebarMenuData = {
  navMenu: {
    overview: MENUS[ROLES.SUPER_USER],
  },
}

/** Menu for a role; unknown or missing roles fall back to the user menu. */
export function getSidebarMenu(role?: string): SidebarMenuData {
  const nav = MENUS[role as RoleName] ?? MENUS[ROLES.USER]
  return { navMenu: { overview: nav } }
}

export { SIDEBAR_MENU }
