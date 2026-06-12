import { useMutation, useQueryClient } from '@tanstack/react-query'

// useAction wraps a write (an oRPC client call) in a React Query mutation that
// invalidates the given query keys on success, so lists/details refresh live
// instead of going stale until a manual reload. Standardizes the pattern across
// the app (replacing fire-and-forget client.X() calls).
//
// Example:
//   const cancel = useAction((runId: string) => client.runs.cancel({ runId }), {
//     invalidate: [orpc.runs.list.key()],
//   })
//   <button onClick={() => cancel.mutate(runId)} disabled={cancel.isPending}>
export function useAction<TArgs, TResult>(
  fn: (args: TArgs) => Promise<TResult>,
  opts?: { invalidate?: readonly unknown[][]; onSuccess?: (result: TResult, args: TArgs) => void },
) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: fn,
    onSuccess: (result, args) => {
      for (const queryKey of opts?.invalidate ?? []) {
        queryClient.invalidateQueries({ queryKey: queryKey as unknown[] })
      }
      opts?.onSuccess?.(result, args)
    },
  })
}
