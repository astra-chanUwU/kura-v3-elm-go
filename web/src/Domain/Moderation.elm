module Domain.Moderation exposing
    ( Action(..)
    , Applied
    , AuditEntry
    , ModerationPost
    , ModerationResult
    , QueuePage
    , State(..)
    , actionDecoder
    , actionLabel
    , actionToString
    , actionsDecoder
    , allStates
    , pageDecoder
    , receiptMatches
    , requiresConfirmation
    , responseDecoder
    , stateDecoder
    , stateFromString
    , stateLabel
    , stateToString
    , targetState
    )

{-| Moderation contracts. Queue rows are metadata-only: no preview or
original URL is decoded, so hidden/rejected media URLs can never reach
an image element from this module. Protected thumbnails arrive only
through the authenticated preview bridge (`Api.ModerationPreview`).
-}

import Json.Decode as Decode exposing (Decoder)


type State
    = Published
    | Pending
    | Hidden
    | Rejected


allStates : List State
allStates =
    [ Published, Pending, Hidden, Rejected ]


stateToString : State -> String
stateToString state =
    case state of
        Published ->
            "published"

        Pending ->
            "pending"

        Hidden ->
            "hidden"

        Rejected ->
            "rejected"


stateLabel : State -> String
stateLabel state =
    case state of
        Published ->
            "Published"

        Pending ->
            "Pending"

        Hidden ->
            "Hidden"

        Rejected ->
            "Rejected"


stateFromString : String -> Maybe State
stateFromString value =
    case value of
        "published" ->
            Just Published

        "pending" ->
            Just Pending

        "hidden" ->
            Just Hidden

        "rejected" ->
            Just Rejected

        _ ->
            Nothing


type Action
    = Approve
    | Pend
    | Hide
    | Reject


actionToString : Action -> String
actionToString action =
    case action of
        Approve ->
            "approve"

        Pend ->
            "pend"

        Hide ->
            "hide"

        Reject ->
            "reject"


actionLabel : Action -> String
actionLabel action =
    case action of
        Approve ->
            "Approve (publish)"

        Pend ->
            "Pend"

        Hide ->
            "Hide"

        Reject ->
            "Reject"


actionFromString : String -> Maybe Action
actionFromString value =
    case value of
        "approve" ->
            Just Approve

        "pend" ->
            Just Pend

        "hide" ->
            Just Hide

        "reject" ->
            Just Reject

        _ ->
            Nothing


targetState : Action -> State
targetState action =
    case action of
        Approve ->
            Published

        Pend ->
            Pending

        Hide ->
            Hidden

        Reject ->
            Rejected


{-| Hide/reject always need an explicit review step, as does any bulk
transition (more than one post), whatever the action.
-}
requiresConfirmation : Action -> List String -> Bool
requiresConfirmation action ids =
    List.length ids > 1 || action == Hide || action == Reject


type alias ModerationPost =
    { id : String
    , mediaType : String
    , width : Int
    , height : Int
    , tags : List String
    , rating : String
    , moderationState : State
    }


type alias QueuePage =
    { posts : List ModerationPost
    , nextCursor : Maybe String
    }


type alias ModerationResult =
    { id : String
    , action : Action
    , previousState : State
    , newState : State
    , changed : Bool
    }


{-| What coordinator code needs to reconcile the Library after a write.
-}
type alias Applied =
    { ids : List String
    , action : Action
    , newState : State
    }


type alias AuditEntry =
    { id : String
    , postId : String
    , action : Action
    , previousState : State
    , newState : State
    , actor : String
    , reason : String
    , createdAt : String
    }


stateDecoder : Decoder State
stateDecoder =
    Decode.string
        |> Decode.andThen
            (\value ->
                case stateFromString value of
                    Just state ->
                        Decode.succeed state

                    Nothing ->
                        Decode.fail ("unknown moderation state: " ++ value)
            )


actionDecoder : Decoder Action
actionDecoder =
    Decode.string
        |> Decode.andThen
            (\value ->
                case actionFromString value of
                    Just action ->
                        Decode.succeed action

                    Nothing ->
                        Decode.fail ("unknown moderation action: " ++ value)
            )


postDecoder : Decoder ModerationPost
postDecoder =
    Decode.map7 ModerationPost
        (Decode.field "id" Decode.string)
        (Decode.field "media_type" Decode.string)
        (Decode.field "width" Decode.int)
        (Decode.field "height" Decode.int)
        (Decode.oneOf [ Decode.field "tags" (Decode.list Decode.string), Decode.succeed [] ])
        (Decode.oneOf [ Decode.field "rating" Decode.string, Decode.succeed "" ])
        (Decode.field "moderation_state" stateDecoder)


pageDecoder : Decoder QueuePage
pageDecoder =
    Decode.map2 QueuePage
        (Decode.field "posts" (Decode.list postDecoder))
        (Decode.field "next_cursor" (Decode.nullable Decode.string))


resultDecoder : Decoder ModerationResult
resultDecoder =
    Decode.map5 ModerationResult
        (Decode.field "id" Decode.string)
        (Decode.field "action" actionDecoder)
        (Decode.field "previous_state" stateDecoder)
        (Decode.field "new_state" stateDecoder)
        (Decode.field "changed" Decode.bool)


responseDecoder : Decoder (List ModerationResult)
responseDecoder =
    Decode.field "posts" (Decode.list resultDecoder)


auditDecoder : Decoder AuditEntry
auditDecoder =
    Decode.map8 AuditEntry
        (Decode.field "id" Decode.string)
        (Decode.field "post_id" Decode.string)
        (Decode.field "action" actionDecoder)
        (Decode.field "previous_state" stateDecoder)
        (Decode.field "new_state" stateDecoder)
        (Decode.field "actor" Decode.string)
        (Decode.oneOf [ Decode.field "reason" Decode.string, Decode.succeed "" ])
        (Decode.field "created_at" Decode.string)


actionsDecoder : Decoder (List AuditEntry)
actionsDecoder =
    Decode.field "actions" (Decode.list auditDecoder)


{-| Exact receipt validation: the returned ids are precisely the
requested set (no duplicates, none missing or extra), every row carries
the requested action, and every new state is that action's target state.
Anything else is not a successful acknowledgement.
-}
receiptMatches : List String -> Action -> List ModerationResult -> Bool
receiptMatches ids action results =
    List.length results
        == List.length ids
        && List.sort ids
        == List.sort (List.map .id results)
        && List.all (\row -> row.action == action && row.newState == targetState action) results
