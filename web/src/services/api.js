const request = async (path, options = {}) => {
  const token = localStorage.getItem("arman.accessToken");
  const response = await fetch(path, {
    headers: {
      "Content-Type": "application/json",
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...(options.headers || {}),
    },
    ...options,
  });

  const body = await response.json().catch(() => null);
  if (!response.ok) {
    throw new Error(body?.message || "The request could not be completed.");
  }
  return body;
};

export const getConfig = () => request("/api/v1/config");
export const getResources = (query = "") =>
  request(`/api/v1/resources?limit=12${query ? `&q=${encodeURIComponent(query)}` : ""}`);

export const login = (email, password) =>
  request("/api/v1/auth/login", {
    method: "POST",
    body: JSON.stringify({ email, password }),
  });

export const register = (name, username, email, password) =>
  request("/api/v1/auth/register", {
    method: "POST",
    body: JSON.stringify({ name, username, email, password }),
  });

export const refresh = (refreshToken) =>
  request("/api/v1/auth/refresh", {
    method: "POST",
    body: JSON.stringify({ refreshToken }),
  });

export const getProfile = () => request("/api/v1/profiles/me");
export const updatePrivacy = (payload) =>
  request("/api/v1/profiles/me", {
    method: "PATCH",
    body: JSON.stringify(payload),
  });
export const getBookmarks = () => request("/api/v1/bookmarks");
export const getMyResources = () => request("/api/v1/resources/mine");
export const createResource = (payload) =>
  request("/api/v1/resources", {
    method: "POST",
    body: JSON.stringify(payload),
  });
export const updateResource = (id, payload) =>
  request(`/api/v1/resources/${id}`, {
    method: "PATCH",
    body: JSON.stringify(payload),
  });
export const deleteResource = (id) =>
  request(`/api/v1/resources/${id}`, { method: "DELETE" });
export const uploadResourceFile = async (file) => {
  const token = localStorage.getItem("arman.accessToken");
  const form = new FormData();
  form.append("file", file);
  const response = await fetch("/api/v1/resources/upload", {
    method: "POST",
    headers: token ? { Authorization: `Bearer ${token}` } : {},
    body: form,
  });
  const body = await response.json().catch(() => null);
  if (!response.ok) throw new Error(body?.message || "The file could not be uploaded.");
  return body;
};
export const getResource = (id) => request(`/api/v1/resources/${id}`);
export const bookmarkResource = (id) =>
  request(`/api/v1/resources/${id}/bookmark`, { method: "POST" });
export const removeBookmark = (id) =>
  request(`/api/v1/resources/${id}/bookmark`, { method: "DELETE" });
export const forgotPassword = (email) =>
  request("/api/v1/auth/forgot-password", {
    method: "POST",
    body: JSON.stringify({ email }),
  });
export const resendVerification = () =>
  request("/api/v1/auth/resend-verification", { method: "POST" });

export const saveSession = (data) => {
  localStorage.setItem("arman.accessToken", data.accessToken);
  localStorage.setItem("arman.refreshToken", data.refreshToken);
  localStorage.setItem("arman.user", JSON.stringify(data.user));
};

export const clearSession = () => {
  localStorage.removeItem("arman.accessToken");
  localStorage.removeItem("arman.refreshToken");
  localStorage.removeItem("arman.user");
};
