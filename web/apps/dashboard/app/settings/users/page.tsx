"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { api, ApiError, type InvitedUser, type User } from "@/lib/api";
import { Badge, Button, ErrorBanner, Field, inputClass } from "@/components/ui";

// Settings > Users (Part I.6): owner/member list, invite. V1 has no
// email infrastructure anywhere in this codebase, so "invite" creates
// the account directly with a generated one-time password shown here —
// share it with the new user out of band, the same "shown once, never
// again" convention the API key creation flow already uses.
export default function UsersSettingsPage() {
  const router = useRouter();
  const [users, setUsers] = useState<User[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  const [email, setEmail] = useState("");
  const [role, setRole] = useState<"owner" | "member">("member");
  const [inviting, setInviting] = useState(false);
  const [inviteError, setInviteError] = useState<string | null>(null);
  const [invited, setInvited] = useState<InvitedUser | null>(null);

  function load() {
    api
      .listUsers()
      .then(setUsers)
      .catch((err) => {
        if (err instanceof ApiError && err.status === 401) {
          router.replace("/login");
          return;
        }
        setError(err instanceof ApiError ? err.message : "Failed to load users.");
      });
  }

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function invite(e: React.FormEvent) {
    e.preventDefault();
    setInviting(true);
    setInviteError(null);
    setInvited(null);
    try {
      const user = await api.inviteUser({ email, role });
      setInvited(user);
      setEmail("");
      load();
    } catch (err) {
      setInviteError(err instanceof ApiError ? err.message : "Failed to invite user.");
    } finally {
      setInviting(false);
    }
  }

  return (
    <div className="space-y-6">
      <ErrorBanner message={error} />

      {users === null && !error && <p className="text-sm text-slate-500">Loading…</p>}

      {users && (
        <div className="divide-y divide-slate-100 rounded-lg border border-slate-200 bg-white">
          {users.map((u) => (
            <div key={u.id} className="flex items-center justify-between px-4 py-3">
              <div>
                <p className="text-sm font-medium text-slate-900">{u.email}</p>
                <p className="text-xs text-slate-500">joined {new Date(u.created_at).toLocaleDateString()}</p>
              </div>
              <Badge tone={u.role === "owner" ? "good" : "neutral"}>{u.role}</Badge>
            </div>
          ))}
        </div>
      )}

      <div className="rounded-lg border border-slate-200 bg-white p-4">
        <p className="mb-3 text-sm font-medium text-slate-900">Invite a user</p>

        {invited && (
          <div className="mb-4 rounded-md bg-emerald-50 px-3 py-2 text-xs text-emerald-800">
            <p className="font-medium">{invited.email} created.</p>
            <p className="mt-1">
              Temporary password (shown once — share it out of band):{" "}
              <span className="font-mono">{invited.temporary_password}</span>
            </p>
          </div>
        )}

        <ErrorBanner message={inviteError} />

        <form onSubmit={invite} className="flex flex-wrap items-end gap-3">
          <div className="min-w-[200px] flex-1">
            <Field label="Email">
              <input className={inputClass} type="email" value={email} onChange={(e) => setEmail(e.target.value)} required />
            </Field>
          </div>
          <div>
            <Field label="Role">
              <select
                className={inputClass}
                value={role}
                onChange={(e) => setRole(e.target.value as "owner" | "member")}
              >
                <option value="member">member</option>
                <option value="owner">owner</option>
              </select>
            </Field>
          </div>
          <Button type="submit" disabled={inviting}>
            {inviting ? "Inviting…" : "Invite"}
          </Button>
        </form>
      </div>
    </div>
  );
}
