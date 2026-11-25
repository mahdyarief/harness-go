import { IconCheck } from '@tabler/icons-react'

export type NavSubItem = {
  title: string
  url: string
  icon?: typeof IconCheck
}

export type NavMainItem = {
  title: string
  url: string
  icon: typeof IconCheck
  isActive?: boolean
  items: NavSubItem[]
}

export type SidebarMenuSection = {
  title: string
  items: NavMainItem[]
}

export type SidebarMenuData = {
  sections: SidebarMenuSection[]
}

export type UserInfo = {
  name: string
  email: string
  avatar: string
}
