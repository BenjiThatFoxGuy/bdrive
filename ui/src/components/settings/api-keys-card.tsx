import { memo, useMemo, useState } from "react"
import {
  Input,
  Radio,
  RadioGroup,
  Spinner,
} from "@heroui/react"
import { Button } from "@heroui/react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import toast from "react-hot-toast"

import { $api } from "@/utils/api"
import { copyDataToClipboard } from "@/utils/common"

function formatDate(value?: string) {
  if (!value) return "Never"
  return new Date(value).toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  })
}

function ApiKeysCardInner() {
  const queryClient = useQueryClient()

  const listOptions = useMemo(() => $api.queryOptions("get", "/users/api-keys"), [])
  const { data, isLoading } = useQuery(listOptions)

  const [name, setName] = useState("")
  const [expiryPreset, setExpiryPreset] = useState<"none" | "7" | "30" | "90">("none")
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
    onSuccess: () => {
      setRevokingID(null)
      queryClient.invalidateQueries({ queryKey: listOptions.queryKey })
      toast.success("API key revoked")
    },
    onError: () => {
      setRevokingID(null)
    },
  })

  return (
    <div className="bg-surface rounded-3xl p-6 border border-border/50">
      <div className="flex items-start justify-between gap-3 mb-4">
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
      <div className="mb-4 rounded-2xl border border-border/40 bg-surface p-4 space-y-3">
          <Input
            variant="secondary"
            value={name} onChange={(e) => setName(e.target.value)}
            placeholder="CI deploy"
          />

          <RadioGroup
            value={expiryPreset} onChange={(v) => setExpiryPreset(v as "none" | "7" | "30" | "90")}
          >
            <Radio value="none">No expiration</Radio>
            <Radio value="7">7 days</Radio>
            <Radio value="30">30 days</Radio>
            <Radio value="90">90 days</Radio>
          </RadioGroup>

          <div className="flex justify-end">
            <Button variant="ghost" onPress={() => setShowCreate(false)}>
              Cancel
            </Button>
            <Button
              variant="secondary" isDisabled={!name.trim()}
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
              Create API Key
            </Button>
          </div>
      </div>
      )}

      {createdKey && (
        <div className="mb-4 rounded-2xl border border-accent/30 bg-accent/10 p-4">
          <p className="text-sm text-foreground mb-2">New key (shown once):</p>
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
      ) : data && data.length > 0 ? (
        <div className="space-y-3">
          {data.map((key) => (
            <div
              key={key.id}
              className="grid grid-cols-1 md:grid-cols-[1.2fr_1fr_1fr_1fr_auto] gap-3 rounded-2xl border border-border/30 bg-surface p-4"
            >
              <div>
                <p className="text-xs text-muted">Name</p>
                <p className="font-medium">{key.name}</p>
              </div>
              <div>
                <p className="text-xs text-muted">Created</p>
                <p>{formatDate(key.createdAt)}</p>
              </div>
              <div>
                <p className="text-xs text-muted">Expires</p>
                <p>{formatDate(key.expiresAt || undefined)}</p>
              </div>
              <div>
                <p className="text-xs text-muted">Last used</p>
                <p>{formatDate(key.lastUsedAt || undefined)}</p>
              </div>
              <div className="flex items-center justify-end">
                <Button
                  variant="ghost"
                  onPress={() => {
                    setRevokingID(key.id)
                    revokeMutation.mutate({ params: { path: { id: key.id } } })
                  }}
                >
                  Revoke
                </Button>
              </div>
            </div>
          ))}
        </div>
      ) : (
        <div className="rounded-2xl border-2 border-dashed border-border/30 bg-surface/50 p-6 text-sm text-muted text-center">
          No API keys yet.
        </div>
      )}
    </div>
  )
}

export const ApiKeysCard = memo(ApiKeysCardInner)
