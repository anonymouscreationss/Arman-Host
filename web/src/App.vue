<script setup>
import { computed, onMounted, ref } from "vue";
import {
  bookmarkResource,
  clearSession,
  createResource,
  deleteResource,
  forgotPassword,
  getBookmarks,
  getConfig,
  getMyResources,
  getProfile,
  getResource,
  getResources,
  login,
  refresh,
  register,
  removeBookmark,
  saveSession,
  uploadResourceFile,
  updateResource,
  updatePrivacy,
} from "./services/api";

const activeView = ref("home");
const config = ref(null);
const resources = ref([]);
const bookmarks = ref([]);
const myResources = ref([]);
const profile = ref(null);
const selectedResource = ref(null);
const loading = ref(true);
const error = ref("");
const searchTerm = ref("");
const authMode = ref("login");
const authError = ref("");
const authForm = ref({ name: "", username: "", email: "", password: "" });
const savingAuth = ref(false);
const savingPrivacy = ref(false);
const signedInUser = ref(JSON.parse(localStorage.getItem("arman.user") || "null"));
const showOnboarding = ref(!localStorage.getItem("arman.onboardingComplete"));
const onboardingStep = ref(0);
const privacy = ref({
  visibility: "public",
  showStatistics: true,
  showAchievements: true,
  allowMessages: true,
});
const resourceForm = ref({
  title: "",
  description: "",
  type: "note",
  subject: "",
  course: "",
  exam: "",
  language: "en",
  visibility: "private",
  fileUrl: "",
});
const resourceFile = ref(null);
const editingResource = ref(null);
const resourceError = ref("");
const savingResource = ref(false);

const featureStates = computed(() => config.value?.data?.features || {});
const isSignedIn = computed(() => Boolean(signedInUser.value));
const isBookmarked = (id) => bookmarks.value.some((item) => item.id === id);

const finishOnboarding = () => {
  localStorage.setItem("arman.onboardingComplete", "true");
  showOnboarding.value = false;
};

const loadDashboard = async (query = "") => {
  loading.value = true;
  error.value = "";
  try {
    const [configResponse, resourcesResponse] = await Promise.all([
      getConfig(),
      getResources(query),
    ]);
    config.value = configResponse;
    resources.value = resourcesResponse.data?.items || [];
    if (isSignedIn.value) {
      const bookmarksResponse = await getBookmarks();
      bookmarks.value = bookmarksResponse.data || [];
      await loadMyResources();
    }
  } catch (requestError) {
    error.value = requestError.message;
  } finally {
    loading.value = false;
  }
};

const loadMyResources = async () => {
  if (!isSignedIn.value) return;
  try {
    const response = await getMyResources();
    myResources.value = response.data || [];
  } catch (requestError) {
    resourceError.value = requestError.message;
  }
};

const restoreSession = async () => {
  const refreshToken = localStorage.getItem("arman.refreshToken");
  if (!refreshToken) return;
  try {
    const response = await refresh(refreshToken);
    saveSession(response.data);
    signedInUser.value = response.data.user;
  } catch {
    clearSession();
    signedInUser.value = null;
  }
};

const submitAuth = async () => {
  savingAuth.value = true;
  authError.value = "";
  try {
    const response =
      authMode.value === "login"
        ? await login(authForm.value.email, authForm.value.password)
        : await register(
            authForm.value.name,
            authForm.value.username,
            authForm.value.email,
            authForm.value.password,
          );
    if (authMode.value === "register") {
      authError.value =
        response.message || "Account created. Check your email to verify it.";
      authMode.value = "login";
      return;
    }
    saveSession(response.data);
    signedInUser.value = response.data.user;
    activeView.value = "home";
    await loadDashboard();
  } catch (requestError) {
    authError.value = requestError.message;
  } finally {
    savingAuth.value = false;
  }
};

const requestPasswordReset = async () => {
  if (!authForm.value.email) {
    authError.value = "Enter your email first.";
    return;
  }
  try {
    const response = await forgotPassword(authForm.value.email);
    authError.value =
      response.message ||
      "If an account exists, recovery instructions will be sent.";
  } catch (requestError) {
    authError.value = requestError.message;
  }
};

const signOut = () => {
  clearSession();
  signedInUser.value = null;
  profile.value = null;
  bookmarks.value = [];
  activeView.value = "home";
};

const openPublish = (resource = null) => {
  editingResource.value = resource;
  resourceError.value = "";
  resourceFile.value = null;
  resourceForm.value = resource
    ? {
        title: resource.title || "",
        description: resource.description || "",
        type: resource.type || "note",
        subject: resource.subject || "",
        course: resource.course || "",
        exam: resource.exam || "",
        language: resource.language || "en",
        visibility: resource.visibility || "private",
        fileUrl: resource.fileUrl || "",
      }
    : {
        title: "",
        description: "",
        type: "note",
        subject: "",
        course: "",
        exam: "",
        language: "en",
        visibility: "private",
        fileUrl: "",
      };
  activeView.value = "publish";
};

const submitResource = async () => {
  savingResource.value = true;
  resourceError.value = "";
  try {
    if (resourceFile.value) {
      const upload = await uploadResourceFile(resourceFile.value);
      resourceForm.value.fileUrl = upload.data?.fileUrl || "";
    }
    const response = editingResource.value
      ? await updateResource(editingResource.value.id, resourceForm.value)
      : await createResource(resourceForm.value);
    await loadMyResources();
    activeView.value = "manage";
    resourceError.value = response.message || "";
  } catch (requestError) {
    resourceError.value = requestError.message;
  } finally {
    savingResource.value = false;
  }
};

const removeMyResource = async (resource) => {
  if (!window.confirm(`Delete “${resource.title}”? This cannot be undone from the app.`)) return;
  try {
    await deleteResource(resource.id);
    myResources.value = myResources.value.filter((item) => item.id !== resource.id);
  } catch (requestError) {
    resourceError.value = requestError.message;
  }
};

const openSettings = async () => {
  if (!isSignedIn.value) {
    activeView.value = "auth";
    return;
  }
  activeView.value = "settings";
  try {
    const response = await getProfile();
    profile.value = response.data;
    privacy.value = {
      visibility: response.data.visibility || "public",
      showStatistics: response.data.showStatistics ?? true,
      showAchievements: response.data.showAchievements ?? true,
      allowMessages: response.data.allowMessages ?? true,
    };
  } catch (requestError) {
    error.value = requestError.message;
  }
};

const savePrivacy = async () => {
  savingPrivacy.value = true;
  try {
    const response = await updatePrivacy(privacy.value);
    profile.value = response.data;
  } catch (requestError) {
    error.value = requestError.message;
  } finally {
    savingPrivacy.value = false;
  }
};

const runSearch = () => loadDashboard(searchTerm.value);

const openResource = async (resource) => {
  try {
    const response = await getResource(resource.id);
    selectedResource.value = response.data;
  } catch (requestError) {
    error.value = requestError.message;
  }
};

const toggleBookmark = async (resource) => {
  if (!isSignedIn.value) {
    activeView.value = "auth";
    authError.value = "Sign in to save resources to your library.";
    return;
  }
  try {
    if (isBookmarked(resource.id)) {
      await removeBookmark(resource.id);
      bookmarks.value = bookmarks.value.filter((item) => item.id !== resource.id);
    } else {
      await bookmarkResource(resource.id);
      bookmarks.value = [...bookmarks.value, resource];
    }
  } catch (requestError) {
    error.value = requestError.message;
  }
};

onMounted(async () => {
  await restoreSession();
  await loadDashboard();
});
</script>

<template>
  <div class="app-shell">
    <header class="topbar">
      <button class="brand" type="button" @click="activeView = 'home'">
        <img class="brand-logo" src="/arman-symbol.png" alt="ARMAN" />
        <span>
          <strong>ARMAN</strong>
          <small>ارمان · Aspire. Learn. Achieve.</small>
        </span>
      </button>
      <nav class="topnav" aria-label="Primary navigation">
        <button v-for="item in ['home', 'explore']" :key="item" type="button" :class="{ active: activeView === item }" @click="activeView = item">
          {{ item }}
        </button>
        <button v-if="isSignedIn" type="button" :class="{ active: activeView === 'saved' }" @click="activeView = 'saved'">saved</button>
        <button v-if="isSignedIn" type="button" :class="{ active: activeView === 'publish' }" @click="openPublish()">publish</button>
        <button v-if="isSignedIn" type="button" :class="{ active: activeView === 'manage' }" @click="loadMyResources(); activeView = 'manage'">manage</button>
        <button v-if="isSignedIn" type="button" :class="{ active: activeView === 'settings' }" @click="openSettings">settings</button>
        <button v-else type="button" :class="{ active: activeView === 'auth' }" @click="activeView = 'auth'">sign in</button>
      </nav>
      <button v-if="isSignedIn" class="user-chip" type="button" @click="openSettings">{{ signedInUser.name }}</button>
      <span v-else class="status-dot" :class="{ ready: !loading && !error }">{{ loading ? "Connecting" : error ? "Needs attention" : "Connected" }}</span>
    </header>

    <main v-if="activeView === 'home'" class="content">
      <section class="hero">
        <div>
          <p class="eyebrow">Your academic home</p>
          <h1>Make progress<br /><em>that feels like yours.</em></h1>
          <p class="hero-copy">Learn with clarity, practice with purpose, and keep every step of your journey in one place.</p>
          <div class="hero-actions">
            <button class="button primary" type="button" @click="activeView = 'explore'">Explore resources</button>
            <button class="button secondary" type="button" @click="activeView = isSignedIn ? 'saved' : 'auth'">{{ isSignedIn ? "Open my library" : "Sign in" }}</button>
          </div>
        </div>
        <div class="hero-art" aria-label="ARMAN aspiration mark">
          <div class="sun"></div><div class="mountain mountain-back"></div><div class="mountain mountain-front"></div><div class="star">✦</div>
        </div>
      </section>

      <section class="section-heading">
        <div><p class="eyebrow">Start where you are</p><h2>Keep moving forward</h2></div>
        <span v-if="signedInUser" class="welcome">Welcome, {{ signedInUser.name }}</span>
      </section>
      <p v-if="error" class="state error-state">{{ error }} <button type="button" @click="loadDashboard()">Try again</button></p>
      <div v-else-if="loading" class="resource-grid"><div v-for="index in 3" :key="index" class="resource-card skeleton"></div></div>
      <div v-else-if="resources.length" class="resource-grid">
        <article v-for="resource in resources" :key="resource.id" class="resource-card clickable" @click="openResource(resource)">
          <div class="card-top"><span class="resource-type">{{ resource.type }}</span><button class="save-button" type="button" @click.stop="toggleBookmark(resource)" :aria-label="isBookmarked(resource.id) ? 'Remove bookmark' : 'Save resource'">{{ isBookmarked(resource.id) ? "★" : "☆" }}</button></div>
          <h3>{{ resource.title }}</h3><p>{{ resource.description || "A resource ready for your next study session." }}</p><footer>{{ resource.subject || "General study" }}</footer>
        </article>
      </div>
      <div v-else class="state empty-state"><span class="state-icon">✦</span><h3>Your library starts here</h3><p>Approved learning resources will appear here as the collection grows.</p></div>
      <section class="feature-strip"><div><p class="eyebrow">Built for the whole journey</p><h2>One calm place to<br />learn, practice, and achieve.</h2></div><div class="feature-list"><span><i>01</i> Learn without losing your place</span><span><i>02</i> Practice with useful feedback</span><span><i>03</i> Keep goals visible and personal</span></div></section>
    </main>

    <main v-else-if="activeView === 'explore'" class="content narrow">
      <p class="eyebrow">Explore</p><h1>Find your next<br /><em>useful thing.</em></h1>
      <p class="hero-copy">Search, save, and return to real learning resources when you are ready.</p>
      <form class="search-bar" @submit.prevent="runSearch"><input v-model="searchTerm" type="search" placeholder="Search notes, subjects, and courses" aria-label="Search resources" /><button class="button primary" type="submit">Search</button></form>
      <div v-if="selectedResource" class="detail-card"><button class="back-link" type="button" @click="selectedResource = null">← Back to results</button><span class="resource-type">{{ selectedResource.type }}</span><h2>{{ selectedResource.title }}</h2><p>{{ selectedResource.description || "Approved ARMAN learning material." }}</p><footer>{{ selectedResource.subject || "General study" }} <button class="save-button" type="button" @click="toggleBookmark(selectedResource)">{{ isBookmarked(selectedResource.id) ? "★ Saved" : "☆ Save" }}</button></footer></div>
      <div v-else class="resource-grid explore-grid"><article v-for="resource in resources" :key="resource.id" class="resource-card clickable" @click="openResource(resource)"><div class="card-top"><span class="resource-type">{{ resource.type }}</span><button class="save-button" type="button" @click.stop="toggleBookmark(resource)">{{ isBookmarked(resource.id) ? "★" : "☆" }}</button></div><h3>{{ resource.title }}</h3><p>{{ resource.description || "Approved ARMAN learning material." }}</p><footer>{{ resource.subject || "General study" }}</footer></article><div v-if="!loading && !resources.length" class="state empty-state"><h3>No resources match yet</h3><p>Try another search, or check back as approved resources are added.</p></div></div>
    </main>

    <main v-else-if="activeView === 'manage'" class="content narrow">
      <div class="section-heading"><div><p class="eyebrow">My resources</p><h1>Share what<br /><em>helps you learn.</em></h1></div><button class="button primary" type="button" @click="openPublish()">Publish resource</button></div>
      <p v-if="resourceError" class="state error-state">{{ resourceError }}</p>
      <div v-if="myResources.length" class="manage-list">
        <article v-for="resource in myResources" :key="resource.id" class="manage-card">
          <div><div class="card-top"><span class="resource-type">{{ resource.type }}</span><span class="status-pill" :class="resource.moderationStatus">{{ resource.moderationStatus }}</span></div><h3>{{ resource.title }}</h3><p>{{ resource.description || "No description added." }}</p><footer>{{ resource.subject || "General study" }} · {{ resource.visibility }}</footer></div>
          <div class="manage-actions"><button class="back-link" type="button" @click="openPublish(resource)">Edit</button><button class="back-link danger-link" type="button" @click="removeMyResource(resource)">Delete</button></div>
        </article>
      </div>
      <div v-else class="state empty-state"><h3>No resources published</h3><p>Publish notes, links, and study materials for review.</p><button class="button primary" type="button" @click="openPublish()">Publish your first resource</button></div>
    </main>

    <main v-else-if="activeView === 'publish'" class="content narrow auth-page">
      <button class="back-link" type="button" @click="activeView = 'manage'">← Back to my resources</button>
      <p class="eyebrow">{{ editingResource ? "Edit resource" : "Publish resource" }}</p>
      <h1>{{ editingResource ? "Improve your" : "Share your" }}<br /><em>next useful thing.</em></h1>
      <form class="auth-card resource-form" @submit.prevent="submitResource">
        <label>Title<input v-model="resourceForm.title" type="text" maxlength="240" required /></label>
        <label>Type<select v-model="resourceForm.type"><option value="note">Note</option><option value="pdf">PDF</option><option value="video">Video</option><option value="link">Link</option><option value="quiz">Quiz</option><option value="flashcard">Flashcard</option><option value="audio">Audio</option></select></label>
        <div class="form-row"><label>Subject<input v-model="resourceForm.subject" type="text" /></label><label>Course<input v-model="resourceForm.course" type="text" /></label></div>
        <label>Description<textarea v-model="resourceForm.description" maxlength="10000" rows="5" placeholder="What will this help another student understand?"></textarea></label>
        <label>Visibility<select v-model="resourceForm.visibility"><option value="private">Private</option><option value="public">Public after approval</option></select></label>
        <label>File (optional)<input type="file" accept=".pdf,.png,.jpg,.jpeg,.webp,.txt,.mp3,.mp4" @change="resourceFile = $event.target.files?.[0] || null" /><small v-if="resourceFile" class="form-note">{{ resourceFile.name }} will be uploaded when you submit.</small></label>
        <p v-if="resourceError" class="form-error">{{ resourceError }}</p>
        <button class="button primary full-width" type="submit" :disabled="savingResource">{{ savingResource ? "Saving…" : editingResource ? "Save changes" : "Submit for review" }}</button>
        <p class="form-note">New and edited resources enter moderation before appearing in public Explore results.</p>
      </form>
    </main>

    <main v-else-if="activeView === 'saved'" class="content narrow"><p class="eyebrow">My library</p><h1>Keep what<br /><em>helps you grow.</em></h1><div class="resource-grid explore-grid"><article v-for="resource in bookmarks" :key="resource.id" class="resource-card clickable" @click="openResource(resource)"><div class="card-top"><span class="resource-type">{{ resource.type }}</span><button class="save-button" type="button" @click.stop="toggleBookmark(resource)">★</button></div><h3>{{ resource.title }}</h3><footer>{{ resource.subject || "General study" }}</footer></article><div v-if="!bookmarks.length" class="state empty-state"><h3>Nothing saved yet</h3><p>Save a resource from Explore to make it available here.</p></div></div></main>

    <main v-else-if="activeView === 'settings'" class="content narrow"><p class="eyebrow">Settings & privacy</p><h1>Keep your<br /><em>space yours.</em></h1><form class="settings-card" @submit.prevent="savePrivacy"><div class="settings-heading"><div><h3>Profile visibility</h3><p>Choose who can view your profile.</p></div><select v-model="privacy.visibility"><option value="public">Public</option><option value="private">Private</option></select></div><label class="toggle-row"><span><strong>Show statistics</strong><small>Let your profile display learning statistics.</small></span><input v-model="privacy.showStatistics" type="checkbox" /></label><label class="toggle-row"><span><strong>Show achievements</strong><small>Let others see achievements you choose to share.</small></span><input v-model="privacy.showAchievements" type="checkbox" /></label><label class="toggle-row"><span><strong>Allow messages</strong><small>Allow other students to send you messages.</small></span><input v-model="privacy.allowMessages" type="checkbox" /></label><div class="settings-actions"><button class="button primary" type="submit" :disabled="savingPrivacy">{{ savingPrivacy ? "Saving…" : "Save privacy settings" }}</button><button class="button secondary" type="button" @click="signOut">Sign out</button></div></form></main>

    <main v-else class="content narrow auth-page"><p class="eyebrow">{{ authMode === "login" ? "Welcome back" : "Start your journey" }}</p><h1>{{ authMode === "login" ? "Return to your" : "Build your" }}<br /><em>next achievement.</em></h1><form class="auth-card" @submit.prevent="submitAuth"><div class="auth-tabs"><button type="button" :class="{ selected: authMode === 'login' }" @click="authMode = 'login'">Sign in</button><button type="button" :class="{ selected: authMode === 'register' }" @click="authMode = 'register'">Create account</button></div><label v-if="authMode === 'register'">Name<input v-model="authForm.name" type="text" autocomplete="name" required /></label><label v-if="authMode === 'register'">Username<input v-model="authForm.username" type="text" autocomplete="username" required /></label><label>Email<input v-model="authForm.email" type="email" autocomplete="email" required /></label><label>Password<input v-model="authForm.password" type="password" autocomplete="current-password" minlength="10" required /></label><p v-if="authError" class="form-error">{{ authError }}</p><button class="button primary full-width" type="submit" :disabled="savingAuth">{{ savingAuth ? "Working…" : authMode === "login" ? "Sign in" : "Create account" }}</button><button v-if="authMode === 'login'" class="back-link" type="button" @click="requestPasswordReset">Forgot password?</button><p class="form-note">Your session is stored locally and refreshed through the ARMAN API. Passwords are never stored in the browser.</p></form></main>

    <div v-if="showOnboarding" class="modal-backdrop"><section class="onboarding-card"><img class="onboarding-logo" src="/arman-logo.png" alt="ARMAN — Aspire. Learn. Achieve." /><p class="eyebrow">Welcome to ARMAN</p><h2>{{ ["A calmer way to learn.", "Keep every next step visible.", "Your journey starts here."][onboardingStep] }}</h2><p>{{ ["Resources, practice, and progress designed around you.", "Save useful material, build momentum, and return when you are ready.", "Create an account when you want persistence across your study sessions."][onboardingStep] }}</p><div class="onboarding-dots"><i v-for="step in 3" :key="step" :class="{ active: onboardingStep === step - 1 }"></i></div><button class="button primary full-width" type="button" @click="onboardingStep < 2 ? onboardingStep++ : finishOnboarding()">{{ onboardingStep < 2 ? "Continue" : "Begin" }}</button><button class="skip-link" type="button" @click="finishOnboarding">Skip for now</button></section></div>

    <footer class="footer"><span>ARMAN · ارمان</span><span>Learn. Understand. Practice. Achieve.</span><span v-if="featureStates.database">Database connected</span></footer>
  </div>
</template>