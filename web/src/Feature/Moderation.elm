module Feature.Moderation exposing
    ( Context
    , Model
    , Msg(..)
    , OutMsg(..)
    , auditEntries
    , context
    , init
    , isOpen
    , items
    , locked
    , nextCursor
    , pendingConfirmationCount
    , previewGeneration
    , previewRequestFor
    , previews
    , auditRequestId
    , queueRequestId
    , queueState
    , selectedIds
    , status
    , uncertain
    , update
    , view
    )

{-| Bounded browser moderation.

The queue pages newest-first by state at 60 rows, selection is this
module's own explicit set capped at 100, and hide/reject or bulk
transitions require an explicit review confirmation. A write is only
settled by an exact receipt (see `Domain.Moderation.receiptMatches`);
any other outcome is uncertain, blocks further writes, and is never
retried — only an explicit reload resolves it. Opening, closing, state
changes, and lock bump request generations so late responses and late
preview object URLs are dropped, and lock clears all private metadata.
-}

import Api.Moderation
import Api.ModerationPreview as Preview
import Dict exposing (Dict)
import Domain.Moderation as Mod exposing (Action(..), AuditEntry, ModerationPost, ModerationResult, QueuePage, State(..))
import Html exposing (Html, button, div, img, input, label, li, option, p, section, select, span, text, ul)
import Html.Attributes exposing (alt, attribute, checked, class, disabled, placeholder, selected, src, type_, value)
import Html.Events exposing (onCheck, onClick, onInput)
import Http
import Json.Decode as Decode
import Set exposing (Set)


pageLimit : Int
pageLimit =
    60


maxSelected : Int
maxSelected =
    100


type alias Context =
    { apiBase : String
    , credential : Maybe String
    , writesEnabled : Bool
    , accessGeneration : Int
    }


context : String -> Maybe String -> Bool -> Int -> Context
context apiBase credential writesEnabled accessGeneration =
    { apiBase = apiBase, credential = credential, writesEnabled = writesEnabled, accessGeneration = accessGeneration }


type alias PendingConfirmation =
    { ids : List String
    , action : Action
    , reason : String
    }


type alias Model =
    { open : Bool
    , state : State
    , items : List ModerationPost
    , nextCursor : Maybe String
    , loading : Bool
    , requestId : Int
    , selected : Set String
    , action : Action
    , reason : String
    , pendingConfirmation : Maybe PendingConfirmation
    , busy : Bool
    , uncertain : Bool
    , status : Maybe String
    , auditPost : Maybe String
    , audit : List AuditEntry
    , auditLoading : Bool
    , auditRequestId : Int
    , previews : Dict String String
    , previewGeneration : Int
    , previewRequestId : Int
    , previewRequests : Dict Int String
    }


init : Model
init =
    { open = False
    , state = Pending
    , items = []
    , nextCursor = Nothing
    , loading = False
    , requestId = 0
    , selected = Set.empty
    , action = Approve
    , reason = ""
    , pendingConfirmation = Nothing
    , busy = False
    , uncertain = False
    , status = Nothing
    , auditPost = Nothing
    , audit = []
    , auditLoading = False
    , auditRequestId = 0
    , previews = Dict.empty
    , previewGeneration = 0
    , previewRequestId = 0
    , previewRequests = Dict.empty
    }


isOpen : Model -> Bool
isOpen m =
    m.open


items : Model -> List ModerationPost
items m =
    m.items


nextCursor : Model -> Maybe String
nextCursor m =
    m.nextCursor


queueState : Model -> State
queueState m =
    m.state


status : Model -> Maybe String
status m =
    m.status


uncertain : Model -> Bool
uncertain m =
    m.uncertain


auditEntries : Model -> List AuditEntry
auditEntries m =
    m.audit


previews : Model -> Dict String String
previews m =
    m.previews


selectedIds : Model -> List String
selectedIds m =
    m.items |> List.map .id |> List.filter (\id -> Set.member id m.selected)


pendingConfirmationCount : Model -> Maybe Int
pendingConfirmationCount m =
    Maybe.map (\pending -> List.length pending.ids) m.pendingConfirmation


queueRequestId : Model -> Int
queueRequestId m =
    m.requestId


auditRequestId : Model -> Int
auditRequestId m =
    m.auditRequestId


previewGeneration : Model -> Int
previewGeneration m =
    m.previewGeneration


previewRequestFor : String -> Model -> Maybe Int
previewRequestFor postId m =
    Dict.toList m.previewRequests
        |> List.filter (\( _, id ) -> id == postId)
        |> List.head
        |> Maybe.map Tuple.first


{-| Lock clears the private queue, audit history, selection, drafts'
pending confirmation, and preview URLs, and invalidates every in-flight
generation. Locked browsers cannot list the private queue.
-}
locked : Model -> Model
locked m =
    { m
        | open = False
        , items = []
        , nextCursor = Nothing
        , loading = False
        , requestId = m.requestId + 1
        , selected = Set.empty
        , pendingConfirmation = Nothing
        , busy = False
        , uncertain = False
        , status = Nothing
        , auditPost = Nothing
        , audit = []
        , auditLoading = False
        , auditRequestId = m.auditRequestId + 1
        , previews = Dict.empty
        , previewGeneration = m.previewGeneration + 1
        , previewRequests = Dict.empty
    }


type Msg
    = OpenRequested
    | CloseRequested
    | StateSelected State
    | ReloadRequested
    | LoadMoreRequested
    | Loaded Int Int State Bool (Result Http.Error QueuePage)
    | ItemToggled String Bool
    | SelectionCleared
    | ActionSelected String
    | ReasonChanged String
    | ReviewRequested
    | ConfirmRequested
    | ConfirmationCancelled
    | Moderated Int (List String) Action (Result Http.Error (List ModerationResult))
    | AuditRequested String
    | AuditClosed
    | AuditLoaded Int Int String (Result Http.Error (List AuditEntry))
    | PreviewEventReceived Decode.Value


type OutMsg
    = ModerationApplied Mod.Applied
    | AccessRevoked


update : Context -> Msg -> Model -> ( Model, Cmd Msg, List OutMsg )
update ctx msg model =
    case msg of
        OpenRequested ->
            if model.open && (model.loading || not (List.isEmpty model.items)) then
                ( model, Cmd.none, [] )

            else if not ctx.writesEnabled then
                ( { model | open = True, items = [], nextCursor = Nothing, status = Just "Unlock to view the moderation queue." }, Cmd.none, [] )

            else
                loadFirstPage ctx model

        CloseRequested ->
            ( invalidate model, Preview.releaseAll, [] )

        StateSelected state ->
            if state == model.state && model.open then
                ( model, Cmd.none, [] )

            else if not ctx.writesEnabled then
                ( { model | state = state, items = [], nextCursor = Nothing, selected = Set.empty, status = Just "Unlock to view the moderation queue." }, Preview.releaseGeneration model.previewGeneration, [] )

            else
                loadFirstPage ctx
                    { model
                        | state = state
                        , items = []
                        , nextCursor = Nothing
                        , selected = Set.empty
                        , pendingConfirmation = Nothing
                        , uncertain = False
                        , auditPost = Nothing
                        , audit = []
                        , auditRequestId = model.auditRequestId + 1
                        , previews = Dict.empty
                        , previewGeneration = model.previewGeneration + 1
                        , previewRequests = Dict.empty
                    }
                    |> withRelease model.previewGeneration

        ReloadRequested ->
            if not ctx.writesEnabled then
                ( { model | open = True, items = [], nextCursor = Nothing, selected = Set.empty, pendingConfirmation = Nothing, previews = Dict.empty, status = Just "Unlock to view the moderation queue." }, Preview.releaseAll, [] )

            else
                loadFirstPage ctx { model | uncertain = False, pendingConfirmation = Nothing }

        LoadMoreRequested ->
            case ( model.open && not model.loading && not model.busy, model.nextCursor ) of
                ( True, Just cursor ) ->
                    let
                        id =
                            model.requestId + 1
                    in
                    ( { model | loading = True, requestId = id, status = Just "Loading moderation queue…" }
                    , Api.Moderation.listQueue ctx.apiBase ctx.credential model.state (Just cursor) pageLimit (Loaded ctx.accessGeneration id model.state True)
                    , []
                    )

                _ ->
                    ( model, Cmd.none, [] )

        Loaded generation id state appending result ->
            if generation /= ctx.accessGeneration || id /= model.requestId || state /= model.state || not model.open then
                ( model, Cmd.none, [] )

            else
                case result of
                    Ok page ->
                        let
                            merged =
                                if appending then
                                    model.items ++ List.filter (\post -> not (List.member post.id (List.map .id model.items))) page.posts

                                else
                                    page.posts

                            settled =
                                { model
                                    | items = merged
                                    , nextCursor = page.nextCursor
                                    , loading = False
                                    , uncertain = False
                                    , selected = Set.filter (\postId -> List.member postId (List.map .id merged)) model.selected
                                    , status = Nothing
                                }
                        in
                        requestPreviews ctx page.posts settled

                    Err (Http.BadStatus 401) ->
                        ( { model | loading = False }, Cmd.none, [ AccessRevoked ] )

                    Err error ->
                        ( { model | loading = False, status = Just ("Moderation queue could not be loaded (HTTP " ++ httpStatus error ++ "). Use Reload to try again.") }, Cmd.none, [] )

        ItemToggled postId isChecked ->
            if model.loading || model.busy || not model.open || not (List.member postId (List.map .id model.items)) then
                ( model, Cmd.none, [] )

            else if isChecked then
                if Set.member postId model.selected then
                    ( model, Cmd.none, [] )

                else if Set.size model.selected >= maxSelected then
                    ( { model | status = Just "At most 100 posts can be chosen at once." }, Cmd.none, [] )

                else
                    ( { model | selected = Set.insert postId model.selected, pendingConfirmation = Nothing, status = Nothing }, Cmd.none, [] )

            else
                ( { model | selected = Set.remove postId model.selected, pendingConfirmation = Nothing }, Cmd.none, [] )

        SelectionCleared ->
            ( { model | selected = Set.empty, pendingConfirmation = Nothing }, Cmd.none, [] )

        ActionSelected raw ->
            ( { model | action = actionFromLabel raw model.action, pendingConfirmation = Nothing }, Cmd.none, [] )

        ReasonChanged value ->
            ( { model | reason = value, pendingConfirmation = Nothing }, Cmd.none, [] )

        ReviewRequested ->
            review ctx model

        ConfirmRequested ->
            case model.pendingConfirmation of
                Nothing ->
                    ( model, Cmd.none, [] )

                Just pending ->
                    if List.sort pending.ids /= List.sort (selectedIds model) || pending.action /= model.action || pending.reason /= model.reason then
                        ( { model | pendingConfirmation = Nothing, status = Just "The selection or draft changed. Review the action again." }, Cmd.none, [] )

                    else
                        sendModeration ctx pending.ids pending.action pending.reason model

        ConfirmationCancelled ->
            ( { model | pendingConfirmation = Nothing, status = Nothing }, Cmd.none, [] )

        Moderated generation ids action result ->
            settleModerated ctx generation ids action result model

        AuditRequested postId ->
            if not ctx.writesEnabled then
                ( { model | status = Just "Unlock to view moderation history." }, Cmd.none, [] )

            else if model.busy || not (List.member postId (List.map .id model.items)) then
                ( model, Cmd.none, [] )

            else
                let
                    id =
                        model.auditRequestId + 1
                in
                ( { model | auditPost = Just postId, audit = [], auditLoading = True, auditRequestId = id, status = Just "Loading moderation history…" }
                , Api.Moderation.listActions ctx.apiBase ctx.credential postId (AuditLoaded ctx.accessGeneration id postId)
                , []
                )

        AuditClosed ->
            ( { model | auditPost = Nothing, audit = [], auditLoading = False, auditRequestId = model.auditRequestId + 1, status = Nothing }, Cmd.none, [] )

        AuditLoaded generation id postId result ->
            if generation /= ctx.accessGeneration || id /= model.auditRequestId || model.auditPost /= Just postId || not model.open then
                ( model, Cmd.none, [] )

            else
                case result of
                    Ok entries ->
                        ( { model | audit = entries, auditLoading = False, status = Nothing }, Cmd.none, [] )

                    Err (Http.BadStatus 401) ->
                        ( { model | auditLoading = False }, Cmd.none, [ AccessRevoked ] )

                    Err error ->
                        ( { model | auditLoading = False, status = Just ("Moderation history could not be loaded (HTTP " ++ httpStatus error ++ ").") }, Cmd.none, [] )

        PreviewEventReceived value ->
            case Decode.decodeValue Preview.eventDecoder value of
                Ok event ->
                    if event.generation /= model.previewGeneration || Dict.get event.requestId model.previewRequests /= Just event.postId || not model.open then
                        ( model, Cmd.none, [] )

                    else
                        case event.url of
                            Just url ->
                                ( { model | previews = Dict.insert event.postId url model.previews }, Cmd.none, [] )

                            Nothing ->
                                ( model, Cmd.none, [] )

                Err _ ->
                    ( model, Cmd.none, [] )


invalidate : Model -> Model
invalidate model =
    { model
        | open = False
        , items = []
        , nextCursor = Nothing
        , loading = False
        , requestId = model.requestId + 1
        , selected = Set.empty
        , pendingConfirmation = Nothing
        , busy = False
        , auditPost = Nothing
        , audit = []
        , auditLoading = False
        , auditRequestId = model.auditRequestId + 1
        , previews = Dict.empty
        , previewGeneration = model.previewGeneration + 1
        , previewRequests = Dict.empty
    }


withRelease : Int -> ( Model, Cmd Msg, List OutMsg ) -> ( Model, Cmd Msg, List OutMsg )
withRelease generation ( model, cmd, outs ) =
    ( model, Cmd.batch [ Preview.releaseGeneration generation, cmd ], outs )


loadFirstPage : Context -> Model -> ( Model, Cmd Msg, List OutMsg )
loadFirstPage ctx model =
    let
        id =
            model.requestId + 1

        opened =
            { model | open = True, loading = True, requestId = id, status = Just "Loading moderation queue…" }
    in
    ( opened
    , Api.Moderation.listQueue ctx.apiBase ctx.credential opened.state Nothing pageLimit (Loaded ctx.accessGeneration id opened.state False)
    , []
    )


requestPreviews : Context -> List ModerationPost -> Model -> ( Model, Cmd Msg, List OutMsg )
requestPreviews ctx posts model =
    if not ctx.writesEnabled then
        ( model, Cmd.none, [] )

    else
        let
            fresh =
                List.filter (\post -> not (Dict.member post.id model.previews) && not (List.member post.id (Dict.values model.previewRequests))) posts

            start =
                model.previewRequestId

            requests =
                List.indexedMap (\index post -> ( start + index + 1, post.id )) fresh

            next =
                { model
                    | previewRequestId = start + List.length requests
                    , previewRequests = List.foldl (\( requestId, postId ) dict -> Dict.insert requestId postId dict) model.previewRequests requests
                }
        in
        ( next
        , Cmd.batch (List.map (\( requestId, postId ) -> Preview.request ctx.apiBase ctx.credential model.previewGeneration requestId postId) requests)
        , []
        )


review : Context -> Model -> ( Model, Cmd Msg, List OutMsg )
review ctx model =
    if not ctx.writesEnabled then
        ( { model | status = Just "Unlock to apply moderation." }, Cmd.none, [] )

    else if model.busy || model.loading then
        ( model, Cmd.none, [] )

    else if model.uncertain then
        ( { model | status = Just "The last action may have been applied. Reload the queue before applying another action; nothing was retried." }, Cmd.none, [] )

    else
        case selectedIds model of
            [] ->
                ( { model | status = Just "Choose at least one post with its checkbox. Nothing is chosen automatically." }, Cmd.none, [] )

            ids ->
                if Mod.requiresConfirmation model.action ids then
                    ( { model | pendingConfirmation = Just { ids = ids, action = model.action, reason = model.reason }, status = Nothing }, Cmd.none, [] )

                else
                    sendModeration ctx ids model.action model.reason model


sendModeration : Context -> List String -> Action -> String -> Model -> ( Model, Cmd Msg, List OutMsg )
sendModeration ctx ids action reason model =
    if not ctx.writesEnabled then
        ( { model | status = Just "Unlock to apply moderation." }, Cmd.none, [] )

    else if model.busy || List.isEmpty ids || List.length ids > maxSelected then
        ( model, Cmd.none, [] )

    else
        ( { model | busy = True, pendingConfirmation = Nothing, status = Just "Applying moderation…" }
        , Api.Moderation.moderate ctx.apiBase ctx.credential ids action reason (Moderated ctx.accessGeneration ids action)
        , []
        )


settleModerated : Context -> Int -> List String -> Action -> Result Http.Error (List ModerationResult) -> Model -> ( Model, Cmd Msg, List OutMsg )
settleModerated ctx generation ids action result model =
    if generation /= ctx.accessGeneration then
        -- The library may still reconcile a proven write after lock, but
        -- no private queue state is restored here.
        case result of
            Ok rows ->
                if Mod.receiptMatches ids action rows then
                    ( model, Cmd.none, [ ModerationApplied { ids = ids, action = action, newState = Mod.targetState action } ] )

                else
                    ( model, Cmd.none, [] )

            Err _ ->
                ( model, Cmd.none, [] )

    else
        case result of
            Ok rows ->
                if Mod.receiptMatches ids action rows then
                    let
                        target =
                            Mod.targetState action

                        kept =
                            if target == model.state then
                                List.map (\post -> if List.member post.id ids then { post | moderationState = target } else post) model.items

                            else
                                List.filter (\post -> not (List.member post.id ids)) model.items
                    in
                    ( { model
                        | busy = False
                        , items = kept
                        , selected = Set.filter (\postId -> List.member postId (List.map .id kept)) model.selected
                        , pendingConfirmation = Nothing
                        , status = Just ("Applied " ++ Mod.actionToString action ++ " to " ++ String.fromInt (List.length ids) ++ " post(s).")
                      }
                    , Cmd.none
                    , [ ModerationApplied { ids = ids, action = action, newState = target } ]
                    )

                else
                    uncertainWrite "The server returned an unexpected moderation receipt." model

            Err (Http.BadStatus 401) ->
                ( { model | busy = False }, Cmd.none, [ AccessRevoked ] )

            Err error ->
                uncertainWrite ("The moderation request failed (HTTP " ++ httpStatus error ++ ").") model


uncertainWrite : String -> Model -> ( Model, Cmd Msg, List OutMsg )
uncertainWrite detail model =
    ( { model
        | busy = False
        , pendingConfirmation = Nothing
        , uncertain = True
        , status = Just (detail ++ " It may or may not have been applied and was not retried. Reload the queue to check before applying another action.")
      }
    , Cmd.none
    , []
    )


actionFromLabel : String -> Action -> Action
actionFromLabel raw fallback =
    case raw of
        "approve" ->
            Approve

        "pend" ->
            Pend

        "hide" ->
            Hide

        "reject" ->
            Reject

        _ ->
            fallback


httpStatus : Http.Error -> String
httpStatus error =
    case error of
        Http.BadStatus code ->
            String.fromInt code

        _ ->
            "network"


{-| Concise accessible view. Images only ever use transient object URLs
from the preview bridge; queue rows themselves carry no media URL.
-}
view : Model -> Html Msg
view model =
    section [ class "panel-section moderation-view", attribute "aria-label" "Moderation queue" ]
        ([ p [ class "panel-section-title" ] [ text "Moderation" ]
         , div [ class "moderation-tabs", attribute "role" "tablist", attribute "aria-label" "Moderation state" ]
            (List.map
                (\state ->
                    button
                        [ class "button button-quiet moderation-tab"
                        , type_ "button"
                        , attribute "role" "tab"
                        , attribute "aria-selected" (boolText (state == model.state))
                        , disabled model.loading
                        , onClick (StateSelected state)
                        ]
                        [ text (Mod.stateLabel state) ]
                )
                Mod.allStates
            )
         , case model.status of
            Just message ->
                p [ class "panel-note", attribute "role" "status" ] [ text message ]

            Nothing ->
                text ""
         ]
            ++ confirmationView model
            ++ [ ul [ class "moderation-list" ]
                    (List.map (itemView model) model.items)
               , if List.isEmpty model.items && not model.loading then
                    p [ class "panel-note" ] [ text "No posts in this state." ]

                 else
                    text ""
               , div [ class "moderation-actions" ]
                    [ label [ class "moderation-field" ]
                        [ span [] [ text "Action" ]
                        , select [ class "moderation-select", onInput ActionSelected, disabled (model.busy || model.loading) ]
                            (List.map
                                (\action -> option [ value (Mod.actionToString action), selected (action == model.action) ] [ text (Mod.actionLabel action) ])
                                [ Approve, Pend, Hide, Reject ]
                            )
                        ]
                    , label [ class "moderation-field" ]
                        [ span [] [ text "Reason (optional)" ]
                        , input [ type_ "text", class "moderation-reason", placeholder "Reason recorded in history", value model.reason, onInput ReasonChanged, disabled model.busy ]
                            []
                        ]
                    , button [ class "button", type_ "button", onClick ReviewRequested, disabled (model.busy || model.loading || model.uncertain) ]
                        [ text ("Apply to " ++ String.fromInt (List.length (selectedIds model)) ++ " chosen") ]
                    , button [ class "button button-quiet", type_ "button", onClick SelectionCleared, disabled (List.isEmpty (selectedIds model)) ] [ text "Clear choice" ]
                    ]
               , p []
                    [ button [ class "button button-quiet", type_ "button", onClick LoadMoreRequested, disabled (model.nextCursor == Nothing || model.loading) ] [ text "Load more" ]
                    , button [ class "button button-quiet", type_ "button", onClick ReloadRequested, disabled model.loading ] [ text "Reload queue" ]
                    , button [ class "button button-quiet", type_ "button", onClick CloseRequested ] [ text "Close moderation" ]
                    ]
               ]
            ++ auditView model
        )


confirmationView : Model -> List (Html Msg)
confirmationView model =
    case model.pendingConfirmation of
        Just pending ->
            [ div [ class "moderation-confirm", attribute "role" "alertdialog", attribute "aria-label" "Confirm moderation action" ]
                [ p [ class "panel-note moderation-confirm-text" ]
                    [ text ("Apply " ++ Mod.actionToString pending.action ++ " to " ++ String.fromInt (List.length pending.ids) ++ " post(s)? This is recorded in moderation history.") ]
                , p []
                    [ button [ class "button", type_ "button", onClick ConfirmRequested, disabled model.busy ] [ text "Confirm" ]
                    , button [ class "button button-quiet", type_ "button", onClick ConfirmationCancelled ] [ text "Cancel" ]
                    ]
                ]
            ]

        Nothing ->
            []


itemView : Model -> ModerationPost -> Html Msg
itemView model post =
    li [ class "moderation-row" ]
        [ label [ class "moderation-check" ]
            [ input
                [ type_ "checkbox"
                , checked (Set.member post.id model.selected)
                , onCheck (ItemToggled post.id)
                , attribute "aria-label" ("Choose post " ++ post.id ++ " for moderation")
                ]
                []
            ]
        , case Dict.get post.id model.previews of
            Just url ->
                img [ class "moderation-thumb", src url, alt ("Protected preview of post " ++ post.id) ] []

            Nothing ->
                span [ class "moderation-thumb moderation-thumb-empty", attribute "aria-hidden" "true" ] [ text "…" ]
        , span [ class "moderation-meta" ]
            [ span [] [ text ("#" ++ post.id ++ " · " ++ post.mediaType ++ " · " ++ String.fromInt post.width ++ "×" ++ String.fromInt post.height) ]
            , span [ class "panel-note" ] [ text (String.join ", " (List.take 8 post.tags)) ]
            ]
        , button [ class "button button-quiet", type_ "button", onClick (AuditRequested post.id), disabled model.busy ] [ text "History" ]
        ]


auditView : Model -> List (Html Msg)
auditView model =
    case model.auditPost of
        Just postId ->
            [ div [ class "moderation-audit", attribute "aria-label" ("Moderation history for post " ++ postId) ]
                [ p [ class "panel-section-title" ] [ text ("History · #" ++ postId) ]
                , if model.auditLoading then
                    p [ class "panel-note" ] [ text "Loading history…" ]

                  else if List.isEmpty model.audit then
                    p [ class "panel-note" ] [ text "No moderation history." ]

                  else
                    ul [ class "moderation-audit-list" ]
                        (List.map
                            (\entry ->
                                li [ class "moderation-audit-row" ]
                                    [ span [] [ text (Mod.actionToString entry.action ++ " · " ++ Mod.stateToString entry.previousState ++ " → " ++ Mod.stateToString entry.newState) ]
                                    , span [ class "panel-note" ] [ text (entry.actor ++ " · " ++ entry.createdAt) ]
                                    , if entry.reason == "" then
                                        text ""

                                      else
                                        span [ class "panel-note" ] [ text entry.reason ]
                                    ]
                            )
                            model.audit
                        )
                , button [ class "button button-quiet", type_ "button", onClick AuditClosed ] [ text "Close history" ]
                ]
            ]

        Nothing ->
            []


boolText : Bool -> String
boolText value =
    if value then
        "true"

    else
        "false"


