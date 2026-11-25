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

import type { NavMainItem, SidebarMenuData, SidebarMenuSection } from '@/types/menu'

import { ROLES } from '@/lib/constants/auth'

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

const sections = (...items: SidebarMenuSection[]): SidebarMenuSection[] => items

const sectionsFor = (role: string): SidebarMenuSection[] => {
  if (role === ROLES.SUPER_USER) {
    return sections(
      { title: 'Menu', items: [NAV_DASHBOARD, NAV_CHAT, NAV_KNOWLEDGE] },
      {
        title: 'Manajemen',
        items: [NAV_TENANTS, NAV_USERS, NAV_MASTER, NAV_TOKENS, NAV_AUDIT],
      },
      { title: 'Platform', items: [NAV_LLM] },
    )
  }
  return sections({ title: 'Menu', items: [NAV_DASHBOARD, NAV_CHAT, NAV_KNOWLEDGE, NAV_TENANTS] })
}

/** Menu for a role; unknown or missing roles fall back to the user menu. */
export function getSidebarMenu(role?: string): SidebarMenuData {
  return { sections: sectionsFor(role ?? '') }
}
