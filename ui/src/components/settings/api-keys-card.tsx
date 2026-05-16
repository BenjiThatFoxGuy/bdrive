import { memo, useMemo, useState } from "react"
import {
  Input,
  Radio,
  RadioGroup,
  Spinner,
} from "@heroui/react"
import { Button } from "@heroui/react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import clsx from "clsx"
import toast from "react-hot-toast"

import { $api } from "@/utils/api"
import { copyDataToClipboard } from "@/utils/common"

function formatDate(value?: string) {
  if (!value) {return "Never"}
  return new Date(value).toLocaleDateString(undefined, {
    day: "numeric",
    month: "short",
    year: "numeric",
  })
}

const expiryOptions = [
  { value: "none", label: "Never" },
  { value: "7", label: "7 days" },
  { value: "30", label: "30 days" },
  { value: "90", label: "90 days" },
  { value: "180", label: "180 days" },
  { value: "365", label: "1 year" },
] as const

function ApiKeysCardInner() {
  const queryClient = useQueryClient()

  const listOptions = useMemo(() => $api.queryOptions("get", "/users/api-keys"), [])
  const { data, isLoading } = useQuery(listOptions)

  const [name, setName] = useState("")
  const [expiryPreset, setExpiryPreset] = useState<"none" | "7" | "30" | "90" | "180" | "365">("none")
  const [createdKey, setCreatedKey] = useState("")
  const [revokingID, setRevokingID] = useState<string | null>(null)
  const [showCreate, setShowCreate] = useState(false)

  const createMutation = $api.useMutation("post", "/users/api-keys", {
    onSuccess: (result) => {
      setName("")
      setExpiryPreset("none")
      setShowCreate(false)
      setCreatedKey(result.key)
      queryClient.invalidateQueries({ queryKey: listOptions.queryKey })
      toast.success("API key created")
    },
  })

  const revokeMutation = $api.useMutation("delete", "/users/api-keys/{id}", {
    onError: () => {
      setRevokingID(null)
    },
    onSuccess: () => {
      setRevokingID(null)
      queryClient.invalidateQueries({ queryKey: listOptions.queryKey })
      toast.success("API key revoked")
    },
  })

  return (
    <div className="bg-surface rounded-3xl p-6 border border-border/50">
      <div className="flex items-start justify-between gap-3 mb-6">
        <div>
          <h3 className="text-xl font-semibold mb-1">API Keys</h3>
          <p className="text-sm text-muted">
            Create and manage personal access keys for automation.
          </p>
        </div>
        {!showCreate && (
          <Button variant="secondary" onPress={() => setShowCreate(true)}>
            Add API Key
          </Button>
        )}
      </div>

      {showCreate && (
      <div className="mb-6 rounded-2xl border border-border/40 bg-surface-secondary p-4 space-y-4">
        <p className="text-sm font-medium">New API Key</p>
          <Input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="e.g. CI deploy"
          />

          <RadioGroup
            value={expiryPreset} onChange={(v) => setExpiryPreset(v as "none" | "7" | "30" | "90" | "180" | "365")}
            orientation="horizontal"
          >
            {expiryOptions.map((opt) => (
              <Radio key={opt.value} value={opt.value}>
                <Radio.Control>
                  <Radio.Indicator />
                </Radio.Control>
                {opt.label}
              </Radio>
            ))}
          </RadioGroup>

          <div className="flex justify-end gap-2 pt-1">
            <Button variant="ghost" onPress={() => setShowCreate(false)}>
              Cancel
            </Button>
            <Button
              isDisabled={!name.trim()}
              onPress={() => {
                const body: { name: string; expiresAt?: string } = { name: name.trim() }
                if (expiryPreset !== "none") {
                  const days = Number(expiryPreset)
                  const expires = new Date(Date.now() + days * 24 * 60 * 60 * 1000)
                  body.expiresAt = expires.toISOString()
                }
                createMutation.mutate({ body })
              }}
            >
              Create
            </Button>
          </div>
      </div>
      )}

      {createdKey && (
        <div className="mb-6 rounded-2xl border border-accent/30 bg-accent/10 p-4">
          <p className="text-sm font-medium mb-2">Key created — copy it now (shown once):</p>
          <div className="rounded-xl border border-border/40 bg-surface p-3 font-mono text-sm break-all mb-3">
            {createdKey}
          </div>
          <div className="flex gap-2 justify-end">
            <Button
              variant="secondary"
              onPress={() => {
                copyDataToClipboard([createdKey])
                toast.success("API key copied")
              }}
            >
              Copy
            </Button>
            <Button variant="ghost" onPress={() => setCreatedKey("")}>Dismiss</Button>
          </div>
        </div>
      )}

      {isLoading ? (
        <div className="flex items-center justify-center py-8">
          <Spinner />
        </div>
      ) : (data && data.length > 0 ? (
        <div className="space-y-2">
          {data.map((key) => {
            const now = Date.now()
            const expired = key.expiresAt && new Date(key.expiresAt).getTime() < now
            return (
              <div
                key={key.id}
                className="flex items-center justify-between gap-4 rounded-2xl bg-surface-secondary p-4"
              >
                <div className="flex items-center gap-3 min-w-0 flex-1">
                  <div
                    className={clsx(
                      "size-2 shrink-0 rounded-full",
                      expired ? "bg-danger" : "bg-success",
                    )}
                  />
                  <div className="min-w-0">
                    <p className="font-medium truncate">{key.name}</p>
                    <p className="text-sm text-muted truncate">
                      Created {formatDate(key.createdAt)}
                      {key.expiresAt && <> &middot; Expires {formatDate(key.expiresAt)}</>}
                      {key.lastUsedAt && <> &middot; Used {formatDate(key.lastUsedAt)}</>}
                    </p>
                  </div>
                </div>
                <Button
                  variant="ghost"
                  size="sm"
                  className="shrink-0 text-muted hover:text-danger"
                  isDisabled={revokingID === key.id}
                  onPress={() => {
                    setRevokingID(key.id)
                    revokeMutation.mutate({ params: { path: { id: key.id } } })
                  }}
                >
                  {revokingID === key.id ? "Revoking..." : "Revoke"}
                </Button>
              </div>
            )
          })}
        </div>
      ) : (
        <div className="rounded-2xl border-2 border-dashed border-border/30 bg-surface/50 p-8 text-center">
          <p className="text-sm text-muted mb-1">No API keys yet</p>
          <p className="text-xs text-muted/60">Create one to access the API programmatically.</p>
        </div>
      ))}
    </div>
  )
}

export const ApiKeysCard = memo(ApiKeysCardInner)
