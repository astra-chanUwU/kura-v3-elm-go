module ModerationTests exposing (allPassed, suite)

import Dict
import Domain.Moderation as Mod
import Feature.Moderation as F
import Http
import Json.Decode as Decode
import Json.Encode as Encode


ctx : F.Context
ctx =
    F.context "" (Just "token") True 7


lockedCtx : F.Context
lockedCtx =
    F.context "" Nothing False 7


post : String -> Mod.ModerationPost
post id =
    { id = id, mediaType = "image/jpeg", width = 10, height = 20, tags = [], rating = "safe", moderationState = Mod.Pending }


step : F.Msg -> F.Model -> ( F.Model, List F.OutMsg )
step msg model =
    let
        ( next, _, outs ) =
            F.update ctx msg model
    in
    ( next, outs )


loaded : F.Model
loaded =
    let
        opening =
            F.init |> step F.OpenRequested |> Tuple.first
    in
    opening
        |> step (F.Loaded 7 (F.queueRequestId opening) Mod.Pending False (Ok { posts = [ post "3", post "2", post "1" ], nextCursor = Just "cursor-1" }))
        |> Tuple.first


chosen : F.Model
chosen =
    loaded |> step (F.ItemToggled "3" True) |> Tuple.first


result : String -> Mod.Action -> Mod.ModerationResult
result id action =
    { id = id, action = action, previousState = Mod.Pending, newState = Mod.targetState action, changed = True }


previewValue : Int -> Int -> String -> String -> Encode.Value
previewValue generation requestId postId url =
    Encode.object
        [ ( "generation", Encode.int generation )
        , ( "requestId", Encode.int requestId )
        , ( "postId", Encode.string postId )
        , ( "url", Encode.string url )
        , ( "error", Encode.null )
        ]


suite : List ( String, Bool )
suite =
    [ ( "moderation page decoder is metadata-only and strict about state"
      , case Decode.decodeString Mod.pageDecoder """{"posts":[{"id":"9","media_type":"image/png","width":1,"height":2,"tags":["a"],"rating":"safe","moderation_state":"hidden","preview_url":"/media/original-secret.jpg","original_url":"/media/original-secret.jpg"}],"next_cursor":"c1"}""" of
            Ok page ->
                List.map .id page.posts == [ "9" ] && page.nextCursor == Just "c1"

            Err _ ->
                False
      )
    , ( "moderation page decoder rejects unknown state"
      , case Decode.decodeString Mod.pageDecoder """{"posts":[{"id":"9","media_type":"image/png","width":1,"height":2,"tags":[],"rating":"safe","moderation_state":"banned"}],"next_cursor":null}""" of
            Err _ ->
                True

            Ok _ ->
                False
      )
    , ( "hide and bulk transitions require confirmation, single approve does not"
      , Mod.requiresConfirmation Mod.Hide [ "1" ]
            && Mod.requiresConfirmation Mod.Reject [ "1" ]
            && Mod.requiresConfirmation Mod.Approve [ "1", "2" ]
            && not (Mod.requiresConfirmation Mod.Approve [ "1" ])
      )
    , ( "receipt must match ids action and new state exactly"
      , Mod.receiptMatches [ "2", "1" ] Mod.Hide [ result "1" Mod.Hide, result "2" Mod.Hide ]
            && not (Mod.receiptMatches [ "1" ] Mod.Hide [ result "1" Mod.Approve ])
            && not (Mod.receiptMatches [ "1" ] Mod.Hide [ result "2" Mod.Hide ])
            && not (Mod.receiptMatches [ "1" ] Mod.Hide [ result "1" Mod.Hide, result "1" Mod.Hide ])
      )
    , ( "queue loads newest page and keeps cursor"
      , List.map .id (F.items loaded) == [ "3", "2", "1" ] && F.nextCursor loaded == Just "cursor-1" && F.queueState loaded == Mod.Pending
      )
    , ( "stale queue generation is dropped"
      , let
            opening =
                F.init |> step F.OpenRequested |> Tuple.first

            ( after, _ ) =
                step (F.Loaded 999 (F.queueRequestId opening) Mod.Pending False (Ok { posts = [ post "9" ], nextCursor = Nothing })) opening
        in
        List.isEmpty (F.items after)
      )
    , ( "stale state response is dropped after state change"
      , let
            ( changed, _ ) =
                step (F.StateSelected Mod.Hidden) loaded

            ( after, _ ) =
                step (F.Loaded 7 (F.queueRequestId changed) Mod.Pending False (Ok { posts = [ post "9" ], nextCursor = Nothing })) changed
        in
        List.isEmpty (F.items after) && F.queueState after == Mod.Hidden
      )
    , ( "selection is explicit and never prefilled"
      , List.isEmpty (F.selectedIds loaded) && F.selectedIds chosen == [ "3" ]
      )
    , ( "hide requires review then exact receipt settles and emits Applied"
      , let
            ( reviewing, _ ) =
                step F.ReviewRequested (chosen |> step (F.ActionSelected "hide") |> Tuple.first)

            ( confirmed, _ ) =
                step F.ConfirmRequested reviewing

            ( settled, outs ) =
                step (F.Moderated 7 [ "3" ] Mod.Hide (Ok [ result "3" Mod.Hide ])) confirmed
        in
        F.pendingConfirmationCount reviewing == Just 1
            && List.isEmpty (F.selectedIds settled)
            && outs == [ F.ModerationApplied { ids = [ "3" ], action = Mod.Hide, newState = Mod.Hidden } ]
      )
    , ( "mismatched receipt is uncertain, blocks review, and explicit reload clears it"
      , let
            ( sent, _ ) =
                step F.ReviewRequested chosen

            ( bad, outs ) =
                step (F.Moderated 7 [ "3" ] Mod.Approve (Ok [ result "2" Mod.Approve ])) sent

            ( blocked, _ ) =
                step F.ReviewRequested bad

            ( reloaded, _ ) =
                step F.ReloadRequested blocked
        in
        F.uncertain bad
            && List.isEmpty outs
            && F.uncertain blocked
            && not (F.uncertain reloaded)
      )
    , ( "failed write is uncertain and never auto-retried (no confirmation pending)"
      , let
            ( bad, _ ) =
                chosen |> step F.ReviewRequested |> Tuple.first |> step (F.Moderated 7 [ "3" ] Mod.Approve (Err Http.NetworkError))
        in
        F.uncertain bad && F.pendingConfirmationCount bad == Nothing
      )
    , ( "401 write emits AccessRevoked"
      , let
            ( _, outs ) =
                chosen |> step F.ReviewRequested |> Tuple.first |> step (F.Moderated 7 [ "3" ] Mod.Approve (Err (Http.BadStatus 401)))
        in
        outs == [ F.AccessRevoked ]
      )
    , ( "lock clears private queue, selection, audit and previews"
      , let
            lockedModel =
                F.locked loaded
        in
        not (F.isOpen lockedModel)
            && List.isEmpty (F.items lockedModel)
            && List.isEmpty (F.selectedIds lockedModel)
            && List.isEmpty (F.auditEntries lockedModel)
            && Dict.isEmpty (F.previews lockedModel)
            && F.nextCursor lockedModel == Nothing
      )
    , ( "locked context cannot list the queue"
      , let
            ( model, _, _ ) =
                F.update lockedCtx F.OpenRequested F.init
        in
        List.isEmpty (F.items model)
      )
    , ( "preview event only applies to the current generation and request"
      , let
            requestId =
                F.previewRequestFor "3" loaded |> Maybe.withDefault -1

            ( withPreview, _ ) =
                step (F.PreviewEventReceived (previewValue (F.previewGeneration loaded) requestId "3" "blob:one")) loaded

            ( stale, _ ) =
                step (F.PreviewEventReceived (previewValue (F.previewGeneration loaded + 9) requestId "3" "blob:stale")) loaded
        in
        Dict.get "3" (F.previews withPreview) == Just "blob:one" && Dict.isEmpty (F.previews stale)
      )
    , ( "audit loads only for the captured post and generation"
      , let
            ( opening, _ ) =
                step (F.AuditRequested "3") loaded

            entry =
                { id = "1", postId = "3", action = Mod.Hide, previousState = Mod.Published, newState = Mod.Hidden, actor = "system", reason = "spam", createdAt = "2026-01-01" }

            ( done, _ ) =
                step (F.AuditLoaded 7 (F.auditRequestId opening) "3" (Ok [ entry ])) opening

            ( stale, _ ) =
                step (F.AuditLoaded 999 (F.auditRequestId opening) "3" (Ok [ entry ])) opening
        in
        F.auditEntries done == [ entry ] && List.isEmpty (F.auditEntries stale)
      )
    ]


allPassed : Bool
allPassed =
    List.all Tuple.second suite
