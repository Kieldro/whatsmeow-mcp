package ops

import (
	"context"

	"github.com/Kieldro/whatsmeow-mcp/internal/wa"
)

// Input types. Fields without omitempty are required.

type groupJIDIn struct {
	GroupJID string `json:"group_jid" jsonschema:"Group JID (…@g.us)"`
}

type communityIn struct {
	CommunityJID string `json:"community_jid" jsonschema:"Community (parent group) JID (…@g.us), see list_communities"`
}

type inviteLinkIn struct {
	GroupJID string `json:"group_jid" jsonschema:"Group JID (…@g.us)"`
	Reset    bool   `json:"reset,omitempty" jsonschema:"Revoke the current link and generate a new one (the old link stops working)"`
}

type linkIn struct {
	Link string `json:"link" jsonschema:"Invite link (https://chat.whatsapp.com/…) or just the code"`
}

type groupDescriptionIn struct {
	GroupJID    string `json:"group_jid" jsonschema:"Group JID (…@g.us)"`
	Description string `json:"description" jsonschema:"New description; an empty string clears it"`
}

type groupPhotoIn struct {
	GroupJID  string `json:"group_jid" jsonschema:"Group JID (…@g.us)"`
	ImagePath string `json:"image_path,omitempty" jsonschema:"Absolute path of a JPEG (ideally square, 640x640). Omit to remove the photo"`
}

type groupToggleIn struct {
	GroupJID string `json:"group_jid" jsonschema:"Group JID (…@g.us)"`
	Enabled  bool   `json:"enabled" jsonschema:"true = admins only, false = everyone"`
}

type joinRequestsIn struct {
	GroupJID     string   `json:"group_jid" jsonschema:"Group JID (…@g.us)"`
	Action       string   `json:"action" jsonschema:"approve or reject"`
	Participants []string `json:"participants" jsonschema:"JIDs or phone numbers of the requesters (from list_group_join_requests)"`
}

type phonesIn struct {
	Phones []string `json:"phones" jsonschema:"Phone numbers with country code, e.g. +1 555 0100 (max 50)"`
}

type usersIn struct {
	Users []string `json:"users" jsonschema:"Phone numbers with country code or user JIDs"`
}

type profilePictureIn struct {
	JID       string `json:"jid" jsonschema:"User phone number/JID, group JID (…@g.us) or community JID"`
	Preview   bool   `json:"preview,omitempty" jsonschema:"Fetch the small thumbnail instead of the full image"`
	Community bool   `json:"community,omitempty" jsonschema:"Set when jid is a community (parent group)"`
}

type userIn struct {
	JID string `json:"jid" jsonschema:"Phone number with country code or user JID (…@s.whatsapp.net or …@lid)"`
}

type locationIn struct {
	Recipient string  `json:"recipient" jsonschema:"Phone number with country code or a JID"`
	Latitude  float64 `json:"latitude" jsonschema:"Latitude in degrees (-90 to 90)"`
	Longitude float64 `json:"longitude" jsonschema:"Longitude in degrees (-180 to 180)"`
	Name      string  `json:"name,omitempty" jsonschema:"Optional place name"`
	Address   string  `json:"address,omitempty" jsonschema:"Optional street address"`
}

type contactCardIn struct {
	Recipient string `json:"recipient" jsonschema:"Phone number with country code or a JID to send the card to"`
	Name      string `json:"name" jsonschema:"Display name on the card"`
	Phone     string `json:"phone" jsonschema:"The card's phone number, with country code (e.g. +1 555 0100)"`
}

type stickerIn struct {
	Recipient   string `json:"recipient" jsonschema:"Phone number with country code or a JID"`
	StickerPath string `json:"sticker_path" jsonschema:"Absolute path of a .webp image (512x512 recommended; animated WebP supported)"`
}

type forwardIn struct {
	ChatJID   string `json:"chat_jid" jsonschema:"Chat JID the message is in"`
	MessageID string `json:"message_id" jsonschema:"ID of the stored message to forward"`
	To        string `json:"to" jsonschema:"Destination: phone number with country code or a JID"`
}

type messageRefIn struct {
	ChatJID   string `json:"chat_jid" jsonschema:"Chat JID"`
	MessageID string `json:"message_id" jsonschema:"Message ID"`
}

type starIn struct {
	ChatJID   string `json:"chat_jid" jsonschema:"Chat JID"`
	MessageID string `json:"message_id" jsonschema:"Message ID"`
	Unstar    bool   `json:"unstar,omitempty" jsonschema:"Remove the star instead"`
}

type deleteForMeIn struct {
	ChatJID     string `json:"chat_jid" jsonschema:"Chat JID"`
	MessageID   string `json:"message_id" jsonschema:"Message ID"`
	DeleteMedia bool   `json:"delete_media,omitempty" jsonschema:"Also delete the downloaded media on your phone"`
}

type presenceIn struct {
	State string `json:"state" jsonschema:"available (online) or unavailable (offline)"`
}

type chatPresenceIn struct {
	ChatJID string `json:"chat_jid" jsonschema:"Chat JID or phone number"`
	State   string `json:"state" jsonschema:"typing, recording (voice note) or paused (stop)"`
}

type chatToggleIn struct {
	ChatJID string `json:"chat_jid" jsonschema:"Chat JID or phone number"`
	Undo    bool   `json:"undo,omitempty" jsonschema:"Reverse the action (unarchive / unpin)"`
}

type muteIn struct {
	ChatJID  string `json:"chat_jid" jsonschema:"Chat JID or phone number"`
	Duration string `json:"duration,omitempty" jsonschema:"How long: e.g. 8h, 1d, 1w; empty or forever = indefinitely"`
	Unmute   bool   `json:"unmute,omitempty" jsonschema:"Unmute instead"`
}

type markChatIn struct {
	ChatJID string `json:"chat_jid" jsonschema:"Chat JID or phone number"`
	Unread  bool   `json:"unread,omitempty" jsonschema:"Mark as unread instead of read"`
}

type disappearingIn struct {
	ChatJID string `json:"chat_jid" jsonschema:"Chat JID, phone number or group JID"`
	Timer   string `json:"timer" jsonschema:"off, 24h, 7d or 90d"`
}

type textStatusIn struct {
	Text       string `json:"text" jsonschema:"Status text"`
	Background string `json:"background,omitempty" jsonschema:"Background color #RRGGBB (default WhatsApp green)"`
}

type mediaStatusIn struct {
	MediaPath string `json:"media_path" jsonschema:"Absolute path of an image (.jpg/.png) or video (.mp4/.mov)"`
	Caption   string `json:"caption,omitempty" jsonschema:"Optional caption"`
}

type listStatusesIn struct {
	Sender string `json:"sender,omitempty" jsonschema:"Only statuses from this phone number or JID"`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum results (default 20, max 200)"`
}

type newsletterRefIn struct {
	Channel string `json:"channel" jsonschema:"Channel JID (…@newsletter) or invite link (https://whatsapp.com/channel/…)"`
}

type newsletterJIDIn struct {
	JID string `json:"jid" jsonschema:"Channel JID (…@newsletter); see list_newsletters or newsletter_info"`
}

type newsletterMessagesIn struct {
	JID    string `json:"jid" jsonschema:"Channel JID (…@newsletter)"`
	Count  int    `json:"count,omitempty" jsonschema:"Posts to fetch (default 20, max 100)"`
	Before int    `json:"before,omitempty" jsonschema:"Page backwards: only posts older than this server_id"`
}

type aboutIn struct {
	About string `json:"about" jsonschema:"New About text (max 139 characters)"`
}

type imagePathIn struct {
	ImagePath string `json:"image_path,omitempty" jsonschema:"Absolute path of a JPEG (square, 640x640 recommended). Omit to remove the photo"`
}

type privacySetIn struct {
	Setting string `json:"setting" jsonschema:"group_add, last_seen, status, profile, read_receipts, online, call_add, messages, defense or stickers"`
	Value   string `json:"value" jsonschema:"all, contacts, contact_blacklist, none, match_last_seen (online), known (call_add), on_standard/off (defense), contact_allowlist (stickers)"`
}

type noInput struct{}

func done(extra map[string]any) map[string]any {
	out := map[string]any{"success": true}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

const confirm = " Confirm with the user first."

// All returns every operation in this package.
func All() []Op {
	return []Op{
		// Groups.
		define("get_group_invite_link", "Get group invite link",
			"Get a group's invite link (requires admin). With reset=true the current link is REVOKED and a new one generated, so anyone holding the old link can no longer join.", Write,
			func(ctx context.Context, svc *wa.Service, in inviteLinkIn) (any, error) {
				return svc.GroupInviteLink(ctx, in.GroupJID, in.Reset)
			}),
		define("get_group_info_from_link", "Preview group from link",
			"Look up a group from its invite link (name, description, size, community) WITHOUT joining it.", Read,
			func(ctx context.Context, svc *wa.Service, in linkIn) (any, error) {
				return svc.GroupInfoFromLink(ctx, in.Link)
			}),
		define("join_group_with_link", "Join group with link",
			"OUTWARD ACTION: join a group through an invite link, as the user. Members will see the user join. Groups that require approval send a join request instead."+confirm, Destructive,
			func(ctx context.Context, svc *wa.Service, in linkIn) (any, error) {
				return svc.JoinGroupWithLink(ctx, in.Link)
			}),
		define("leave_group", "Leave group",
			"DESTRUCTIVE: leave a group. Members see the user leave; rejoining needs a new invite."+confirm, Destructive,
			func(ctx context.Context, svc *wa.Service, in groupJIDIn) (any, error) {
				return done(nil), svc.LeaveGroup(ctx, in.GroupJID)
			}),
		define("set_group_description", "Set group description",
			"Change a group's description (topic), visible to all members. Empty clears it. May require admin.", Destructive,
			func(ctx context.Context, svc *wa.Service, in groupDescriptionIn) (any, error) {
				return done(nil), svc.SetGroupDescription(ctx, in.GroupJID, in.Description)
			}),
		define("set_group_photo", "Set group photo",
			"Change a group's photo from a local JPEG, or remove it when image_path is omitted. Visible to all members. May require admin.", Destructive,
			func(ctx context.Context, svc *wa.Service, in groupPhotoIn) (any, error) {
				id, err := svc.SetGroupPhoto(ctx, in.GroupJID, in.ImagePath)
				return done(map[string]any{"picture_id": id}), err
			}),
		define("set_group_announce", "Set admins-only messages",
			"Change who can send messages in a group: enabled=true means ONLY ADMINS can send, false lets everyone send. Requires admin.", Destructive,
			func(ctx context.Context, svc *wa.Service, in groupToggleIn) (any, error) {
				return done(map[string]any{"admins_only_messages": in.Enabled}), svc.SetGroupAnnounce(ctx, in.GroupJID, in.Enabled)
			}),
		define("set_group_locked", "Set admins-only group edits",
			"Change who can edit group info (name, description, photo): enabled=true means ONLY ADMINS, false lets everyone. Requires admin.", Destructive,
			func(ctx context.Context, svc *wa.Service, in groupToggleIn) (any, error) {
				return done(map[string]any{"admins_only_edits": in.Enabled}), svc.SetGroupLocked(ctx, in.GroupJID, in.Enabled)
			}),
		define("list_group_join_requests", "List join requests",
			"List pending requests to join a group that requires admin approval (requires admin).", Read,
			func(ctx context.Context, svc *wa.Service, in groupJIDIn) (any, error) {
				return svc.ListJoinRequests(ctx, in.GroupJID)
			}),
		define("update_group_join_requests", "Approve/reject join requests",
			"Approve (adds them to the group) or reject pending join requests. Requires admin.", Destructive,
			func(ctx context.Context, svc *wa.Service, in joinRequestsIn) (any, error) {
				r, err := svc.UpdateJoinRequests(ctx, in.GroupJID, in.Action, in.Participants)
				return done(map[string]any{"results": r}), err
			}),
		define("list_communities", "List communities",
			"List the communities this account is in, with the linked groups the account has joined (fetched live).", Read,
			func(ctx context.Context, svc *wa.Service, _ noInput) (any, error) {
				return svc.ListCommunities(ctx)
			}),
		define("list_community_groups", "List community groups",
			"List every group linked to a community, including ones the account has not joined.", Read,
			func(ctx context.Context, svc *wa.Service, in communityIn) (any, error) {
				return svc.CommunityGroups(ctx, in.CommunityJID)
			}),

		// Contacts.
		define("is_on_whatsapp", "Check numbers on WhatsApp",
			"Check which phone numbers are registered on WhatsApp and get their JIDs. Use sparingly: bulk lookups can get an account flagged.", Read,
			func(ctx context.Context, svc *wa.Service, in phonesIn) (any, error) {
				return svc.IsOnWhatsApp(ctx, in.Phones)
			}),
		define("get_user_info", "Get user info",
			"Get users' About text, profile picture ID, LID, device count and verified business name.", Read,
			func(ctx context.Context, svc *wa.Service, in usersIn) (any, error) {
				return svc.GetUserInfo(ctx, in.Users)
			}),
		define("get_profile_picture", "Get profile picture",
			"Download a user's, group's or community's profile photo into the media directory and return the local file path.", Write,
			func(ctx context.Context, svc *wa.Service, in profilePictureIn) (any, error) {
				return svc.GetProfilePicture(ctx, in.JID, in.Preview, in.Community)
			}),
		define("block_contact", "Block contact",
			"DESTRUCTIVE: block a user. They can no longer message or call the user, or see their last seen/profile updates."+confirm, Destructive,
			func(ctx context.Context, svc *wa.Service, in userIn) (any, error) {
				bl, err := svc.SetBlocked(ctx, in.JID, true)
				return done(map[string]any{"blocklist": bl}), err
			}),
		define("unblock_contact", "Unblock contact",
			"Unblock a previously blocked user."+confirm, Destructive,
			func(ctx context.Context, svc *wa.Service, in userIn) (any, error) {
				bl, err := svc.SetBlocked(ctx, in.JID, false)
				return done(map[string]any{"blocklist": bl}), err
			}),
		define("list_blocklist", "List blocked contacts",
			"List the users this account has blocked, with names where known.", Read,
			func(ctx context.Context, svc *wa.Service, _ noInput) (any, error) {
				return svc.Blocklist(ctx)
			}),

		// Messages.
		define("send_location", "Send location",
			"Send a location pin (latitude/longitude with optional name and address) as the user. Returns the message id and chat JID."+confirm, Write,
			func(ctx context.Context, svc *wa.Service, in locationIn) (any, error) {
				return svc.SendLocation(ctx, in.Recipient, in.Latitude, in.Longitude, in.Name, in.Address)
			}),
		define("send_contact_card", "Send contact card",
			"Send a contact card (vCard) built from a name and phone number, as the user. Returns the message id and chat JID."+confirm, Write,
			func(ctx context.Context, svc *wa.Service, in contactCardIn) (any, error) {
				return svc.SendContactCard(ctx, in.Recipient, in.Name, in.Phone)
			}),
		define("send_sticker", "Send sticker",
			"Send a local .webp image as a sticker, as the user. Returns the message id and chat JID."+confirm, Write,
			func(ctx context.Context, svc *wa.Service, in stickerIn) (any, error) {
				return svc.SendSticker(ctx, in.Recipient, in.StickerPath)
			}),
		define("forward_message", "Forward message",
			"Forward a stored message (text or media) to another chat, marked as Forwarded, as the user. Locations/contacts/polls are forwarded as their text summary. Returns the new message id and chat JID."+confirm, Write,
			func(ctx context.Context, svc *wa.Service, in forwardIn) (any, error) {
				return svc.Forward(ctx, in.ChatJID, in.MessageID, in.To)
			}),
		define("star_message", "Star message",
			"Star (or with unstar=true, unstar) a message on this account's devices. Private; others are not notified.", Write,
			func(ctx context.Context, svc *wa.Service, in starIn) (any, error) {
				return done(map[string]any{"starred": !in.Unstar}), svc.Star(ctx, in.ChatJID, in.MessageID, !in.Unstar)
			}),
		define("delete_message_for_me", "Delete message for me",
			"DESTRUCTIVE: delete a message from this account's devices only (others still see it). Cannot be undone; also removed from local history. To delete for everyone use delete_message.", Destructive,
			func(ctx context.Context, svc *wa.Service, in deleteForMeIn) (any, error) {
				return done(map[string]any{"id": in.MessageID}), svc.DeleteForMe(ctx, in.ChatJID, in.MessageID, in.DeleteMedia)
			}),

		// Presence.
		define("send_presence", "Set online presence",
			"Set the account's global presence: available (shows online to contacts) or unavailable.", Write,
			func(ctx context.Context, svc *wa.Service, in presenceIn) (any, error) {
				return done(nil), svc.SendPresence(ctx, in.State)
			}),
		define("send_chat_presence", "Show typing",
			"Show typing… or recording audio… in a chat, or stop it (paused). The other side sees it.", Write,
			func(ctx context.Context, svc *wa.Service, in chatPresenceIn) (any, error) {
				return done(nil), svc.SendChatPresence(ctx, in.ChatJID, in.State)
			}),
		define("subscribe_presence", "Subscribe to presence",
			"Ask WhatsApp to push a user's online/last-seen updates; read them with get_last_seen. Works best after send_presence available.", Write,
			func(ctx context.Context, svc *wa.Service, in userIn) (any, error) {
				return done(nil), svc.SubscribePresence(ctx, in.JID)
			}),
		define("get_last_seen", "Get last seen",
			"Return the latest online/last-seen update received for a user since subscribe_presence (in-memory; empty until an update arrives; hidden if the user hides last seen).", Read,
			func(ctx context.Context, svc *wa.Service, in userIn) (any, error) {
				return svc.GetLastSeen(ctx, in.JID)
			}),

		// Chats (app state, synced to the phone).
		define("archive_chat", "Archive chat",
			"Archive a chat (undo=true unarchives) on all of the user's devices. Archiving also unpins it.", Write,
			func(ctx context.Context, svc *wa.Service, in chatToggleIn) (any, error) {
				return done(map[string]any{"archived": !in.Undo}), svc.ArchiveChat(ctx, in.ChatJID, !in.Undo)
			}),
		define("pin_chat", "Pin chat",
			"Pin a chat to the top of the chat list (undo=true unpins) on all of the user's devices.", Write,
			func(ctx context.Context, svc *wa.Service, in chatToggleIn) (any, error) {
				return done(map[string]any{"pinned": !in.Undo}), svc.PinChat(ctx, in.ChatJID, !in.Undo)
			}),
		define("mute_chat", "Mute chat",
			"Mute a chat's notifications for a duration (8h, 1w, forever) or unmute it, on all of the user's devices.", Write,
			func(ctx context.Context, svc *wa.Service, in muteIn) (any, error) {
				st, err := svc.MuteChat(ctx, in.ChatJID, in.Duration, in.Unmute)
				return done(map[string]any{"state": st}), err
			}),
		define("mark_chat_read", "Mark chat read/unread",
			"Mark a whole chat as read, or as unread (unread=true), on all of the user's devices. To send read receipts for specific messages use mark_read.", Write,
			func(ctx context.Context, svc *wa.Service, in markChatIn) (any, error) {
				return done(map[string]any{"read": !in.Unread}), svc.MarkChatRead(ctx, in.ChatJID, !in.Unread)
			}),
		define("set_disappearing_timer", "Set disappearing messages",
			"Change a chat's disappearing-messages timer (off, 24h, 7d, 90d). Everyone in the chat is notified of the change.", Destructive,
			func(ctx context.Context, svc *wa.Service, in disappearingIn) (any, error) {
				d, err := svc.SetDisappearingTimer(ctx, in.ChatJID, in.Timer)
				return done(map[string]any{"seconds": int(d.Seconds())}), err
			}),

		// Status (stories).
		define("post_text_status", "Post text status",
			"OUTWARD ACTION: post a text Status (story) visible to the user's contacts per their status privacy settings, for 24 hours. Returns the message id."+confirm, Destructive,
			func(ctx context.Context, svc *wa.Service, in textStatusIn) (any, error) {
				return svc.PostTextStatus(ctx, in.Text, in.Background)
			}),
		define("post_media_status", "Post media status",
			"OUTWARD ACTION: post an image or video Status (story) with optional caption, visible to the user's contacts for 24 hours. Returns the message id."+confirm, Destructive,
			func(ctx context.Context, svc *wa.Service, in mediaStatusIn) (any, error) {
				return svc.PostMediaStatus(ctx, in.MediaPath, in.Caption)
			}),
		define("list_statuses", "List statuses",
			"List recent Status (story) posts seen by this client, newest first, from the local store. Media can be fetched with download_media (chat_jid status@broadcast).", Read,
			func(ctx context.Context, svc *wa.Service, in listStatusesIn) (any, error) {
				return svc.ListStatuses(ctx, in.Sender, in.Limit)
			}),

		// Channels (newsletters).
		define("list_newsletters", "List channels",
			"List the WhatsApp channels (newsletters) this account follows.", Read,
			func(ctx context.Context, svc *wa.Service, _ noInput) (any, error) {
				return svc.ListNewsletters(ctx)
			}),
		define("newsletter_info", "Channel info",
			"Get a channel's name, description, subscriber count and JID from its JID or invite link.", Read,
			func(ctx context.Context, svc *wa.Service, in newsletterRefIn) (any, error) {
				return svc.NewsletterInfo(ctx, in.Channel)
			}),
		define("follow_newsletter", "Follow channel",
			"Follow a channel as the user."+confirm, Destructive,
			func(ctx context.Context, svc *wa.Service, in newsletterJIDIn) (any, error) {
				return done(nil), svc.FollowNewsletter(ctx, in.JID, true)
			}),
		define("unfollow_newsletter", "Unfollow channel",
			"Unfollow a channel."+confirm, Destructive,
			func(ctx context.Context, svc *wa.Service, in newsletterJIDIn) (any, error) {
				return done(nil), svc.FollowNewsletter(ctx, in.JID, false)
			}),
		define("newsletter_messages", "Channel posts",
			"Fetch recent posts of a channel (text, media type, views, reactions) live from WhatsApp.", Read,
			func(ctx context.Context, svc *wa.Service, in newsletterMessagesIn) (any, error) {
				return svc.NewsletterMessages(ctx, in.JID, in.Count, in.Before)
			}),

		// Profile and privacy.
		define("set_status_message", "Set About text",
			"Change the user's profile About text, visible to others per privacy settings."+confirm, Destructive,
			func(ctx context.Context, svc *wa.Service, in aboutIn) (any, error) {
				return done(nil), svc.SetStatusMessage(ctx, in.About)
			}),
		define("set_profile_picture", "Set profile picture",
			"Change the user's own profile photo from a local JPEG, or remove it when image_path is omitted. Visible to contacts."+confirm, Destructive,
			func(ctx context.Context, svc *wa.Service, in imagePathIn) (any, error) {
				id, err := svc.SetProfilePicture(ctx, in.ImagePath)
				return done(map[string]any{"picture_id": id}), err
			}),
		define("get_privacy_settings", "Get privacy settings",
			"Get the account's privacy settings (last seen, online, profile photo, about/status, read receipts, group add, calls, messages, stickers).", Read,
			func(ctx context.Context, svc *wa.Service, _ noInput) (any, error) {
				return svc.GetPrivacySettings(ctx)
			}),
		define("set_privacy_setting", "Change privacy setting",
			"Change one of the account's privacy settings and return all settings."+confirm, Destructive,
			func(ctx context.Context, svc *wa.Service, in privacySetIn) (any, error) {
				return svc.SetPrivacySetting(ctx, in.Setting, in.Value)
			}),
	}
}
