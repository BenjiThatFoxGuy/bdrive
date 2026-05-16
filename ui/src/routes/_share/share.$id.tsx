import type { ShareListParams } from "@/types";
import { createFileRoute } from "@tanstack/react-router";
import { shareQueries } from "@/utils/query-options";
import { ErrorView } from "@/components/error-view";
import { $api } from "@/utils/api";

const shareUnlockStorageKey = (id: string) => `share-unlocked:${id}`;

export const Route = createFileRoute("/_share/share/$id")({
  validateSearch: (search: Record<string, unknown>) =>
    search as {
      path?: string;
    },
  loaderDeps: ({ search }) => search,
  loader: async ({ context: { queryClient }, params: { id }, deps }) => {
    const res = await queryClient.ensureQueryData(
      $api.queryOptions("get", "/shares/{id}", {
        params: {
          path: {
            id,
          },
        },
      }),
    );
    const unlocked = JSON.parse(sessionStorage.getItem(shareUnlockStorageKey(id)) || "false");
    const queryParams = {
      id,
      path: deps.path || "",
    } as ShareListParams;

    if (res.protected && !unlocked) {
      return;
    }
    try {
      await queryClient.ensureInfiniteQueryData(shareQueries.list(queryParams));
    } catch {
      sessionStorage.removeItem(shareUnlockStorageKey(id));
    }
  },
  wrapInSuspense: true,
  errorComponent: ({ error }) => {
    return <ErrorView message={error.message} />;
  },
});
