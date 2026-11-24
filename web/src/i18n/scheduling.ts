// What a Workspace's unschedulable Pods mean, by compute mode (status.scheduling).
// Screens are in English; the explanation comes in Vietnamese too.
import type { Schemas } from '@/api/client'

type Text = { en: string; vi: string }

export const schedulingHint: Record<Schemas['WorkspaceCompute']['mode'], Text> = {
  Shared: {
    en:
      'The shared pool has no node with room for them right now. They start by themselves when room frees up. ' +
      'To start them sooner, lower their CPU or memory requests, or tell a Platform Admin.',
    vi:
      'Pool dùng chung hiện không có node nào còn đủ chỗ cho các Pod này. Pod sẽ tự chạy khi có chỗ. ' +
      'Muốn chạy sớm hơn, hãy giảm requests CPU hoặc bộ nhớ của Pod, hoặc báo Platform Admin.',
  },
  Dedicated: {
    en:
      "Your Organization's node does not have room for them. They start when other Pods on it end; " +
      'lower their CPU or memory requests, or remove Pods you no longer need.',
    vi:
      'Node riêng của Organization không còn đủ chỗ cho các Pod này. Pod sẽ chạy khi các Pod khác trên node kết thúc; ' +
      'hãy giảm requests CPU hoặc bộ nhớ, hoặc xoá các Pod không còn cần.',
  },
}
