module Feature.Tags exposing
    ( Context
    , Model
    , Msg(..)
    , OutMsg(..)
    , Revert(..)
    , activeChanged
    , addDraft
    , globalTagsChanged
    , context
    , errorToString
    , init
    , locked
    , noteVersion
    , removeDraft
    , requestId
    , revertConfirm
    , revertPending
    , revertStatus
    , saving
    , splitTagDraft
    , status
    , targetsFor
    , update
    )

{-| Tag editing and reverting: drafts, version-checked submissions,
confirmation, and stale-response rejection.

The feature owns the drafts, the saving flag, the status notices, the
request generation, the known tag versions, and the revert flow. The
revert flow is one explicit state — idle, awaiting confirmation, or
reverting — instead of two loosely related flags.

Result integration stays with the caller: `update` reports saved edits
as `OutMsg` values and the page merges tag lists into its sequence and
reloads the active detail. Selection, detail caches, and routing stay
with the page; per-call page facts arrive through `Context`.
-}

import Api.Error as ApiError
import Api.Post
import Dict exposing (Dict)
import Domain.Post exposing (PostDetail, TagEditResponse, TagEditTarget)
import Http


{-| The revert flow. At most one revert is ever armed or in flight:
arming a confirmation while a revert runs is refused.
-}
type Revert
    = Idle
    | Confirming Int
    | Reverting Int


type alias Model =
    { addDraft : String
    , removeDraft : String
    , saving : Bool
    , status : Maybe String
    , requestId : Int
    , versions : Dict String Int
    , revert : Revert
    , revertStatus : Maybe String
    }


{-| Per-call page facts. Tag reads are public but edits are credentialed;
`selectedIds` are the current edit targets, `activeDetail` feeds the
revert history check, and `detailVersion` resolves versions for posts
whose edits never reported one (the feature falls back to it before 0).
-}
type alias Context =
    { apiBase : String
    , credential : Maybe String
    , writesEnabled : Bool
    , accessGeneration : Int
    , selectedIds : List String
    , activeId : Maybe String
    , activeDetail : Maybe PostDetail
    , detailVersion : String -> Maybe Int
    }


context : String -> Maybe String -> Bool -> Int -> List String -> Maybe String -> Maybe PostDetail -> (String -> Maybe Int) -> Context
context apiBase credential writesEnabled accessGeneration selectedIds activeId activeDetail detailVersion =
    { apiBase = apiBase
    , credential = credential
    , writesEnabled = writesEnabled
    , accessGeneration = accessGeneration
    , selectedIds = selectedIds
    , activeId = activeId
    , activeDetail = activeDetail
    , detailVersion = detailVersion
    }


type Msg
    = AddChanged String
    | RemoveChanged String
    | SaveRequested
    | RevertRequested Int
    | RevertConfirmed Int
    | RevertCancelled
    | Completed Int Int (List String) (Result Http.Error TagEditResponse)


{-| `Saved` carries the edited posts for the sequence merge and the
active post for the detail reload. `StaleResponse` carries the request's
post ids so the page can reconcile them after the access epoch moved on.
-}
type OutMsg
    = Saved TagEditResponse (Maybe String)
    | StaleResponse (List String)
    | AccessRevoked


init : Model
init =
    { addDraft = ""
    , removeDraft = ""
    , saving = False
    , status = Nothing
    , requestId = 0
    , versions = Dict.empty
    , revert = Idle
    , revertStatus = Nothing
    }


addDraft : Model -> String
addDraft model =
    model.addDraft


removeDraft : Model -> String
removeDraft model =
    model.removeDraft


saving : Model -> Bool
saving model =
    model.saving


status : Model -> Maybe String
status model =
    model.status


requestId : Model -> Int
requestId model =
    model.requestId


revertStatus : Model -> Maybe String
revertStatus model =
    model.revertStatus


revertConfirm : Model -> Maybe Int
revertConfirm model =
    case model.revert of
        Confirming version ->
            Just version

        _ ->
            Nothing


revertPending : Model -> Maybe Int
revertPending model =
    case model.revert of
        Reverting version ->
            Just version

        _ ->
            Nothing


{-| Record a tag version sighting (for example from a loaded detail) so
later edits carry version checks.
-}
noteVersion : String -> Int -> Model -> Model
noteVersion postId version model =
    { model | versions = Dict.insert postId version model.versions }


{-| The active post changed: drafts, notices, and an armed confirmation
reset, while an in-flight revert survives untouched.
-}
activeChanged : Model -> Model
activeChanged model =
    { model
        | addDraft = ""
        , removeDraft = ""
        , status = Nothing
        , revert =
            case model.revert of
                Confirming _ ->
                    Idle

                other ->
                    other
        , revertStatus = Nothing
    }


{-| Credentials went away: in-flight and armed work clears because its
completions will arrive with an obsolete generation and be ignored.
-}
locked : Model -> Model
locked model =
    { model | saving = False, revert = Idle }


{-| A global rename/merge changed every tag version: cached versions,
drafts, and confirmations are stale and in-flight edits are abandoned.
-}
globalTagsChanged : Model -> Model
globalTagsChanged model =
    { model | addDraft = "", removeDraft = "", status = Nothing, revertStatus = Nothing, revert = Idle, versions = Dict.empty, requestId = model.requestId + 1, saving = False }


resolveTarget : Model -> Context -> String -> TagEditTarget
resolveTarget model ctx postId =
    { id = postId
    , version =
        case Dict.get postId model.versions of
            Just version ->
                version

            Nothing ->
                ctx.detailVersion postId |> Maybe.withDefault 0
    }


{-| Version-checked edit targets for the context's selection. Noted
versions win, then cached detail versions, then zero.
-}
targetsFor : Context -> Model -> List TagEditTarget
targetsFor ctx model =
    List.map (resolveTarget model ctx) ctx.selectedIds


update : Context -> Msg -> Model -> ( Model, Cmd Msg, List OutMsg )
update ctx msg model =
    case msg of
        AddChanged value ->
            ( { model | addDraft = value, status = Nothing }, Cmd.none, [] )

        RemoveChanged value ->
            ( { model | removeDraft = value, status = Nothing }, Cmd.none, [] )

        SaveRequested ->
            saveTags ctx model

        RevertRequested targetVersion ->
            requestRevert ctx targetVersion model

        RevertConfirmed targetVersion ->
            confirmRevert ctx targetVersion model

        RevertCancelled ->
            case model.revert of
                Confirming _ ->
                    ( { model | revert = Idle, revertStatus = Nothing }, Cmd.none, [] )

                _ ->
                    -- An in-flight revert keeps running; only its notice
                    -- clears. Idle needs nothing.
                    ( { model | revertStatus = Nothing }, Cmd.none, [] )

        Completed generation id postIds result ->
            if generation /= ctx.accessGeneration then
                ( model, Cmd.none, [ StaleResponse postIds ] )

            else if id /= model.requestId then
                ( model, Cmd.none, [] )

            else
                case result of
                    Ok response ->
                        ( settled response model
                        , Cmd.none
                        , [ Saved response ctx.activeId ]
                        )

                    Err (Http.BadStatus 401) ->
                        ( model, Cmd.none, [ AccessRevoked ] )

                    Err error ->
                        ( failed error model, Cmd.none, [] )


{-| Merge the saved versions, clear the drafts, and settle the notices.
Success clears an armed confirmation as well as an in-flight revert.
-}
settled : TagEditResponse -> Model -> Model
settled response model =
    let
        versions =
            List.foldl (\post versions_ -> Dict.insert post.id post.version versions_) model.versions response.posts

        wasRevert =
            case model.revert of
                Reverting _ ->
                    True

                _ ->
                    False

        pendingVersion =
            revertPending model
    in
    { model
        | versions = versions
        , saving = False
        , revert = Idle
        , status =
            if wasRevert then
                Nothing

            else
                Just "Tags saved."
        , revertStatus =
            case pendingVersion of
                Just v ->
                    Just ("Reverted to v" ++ String.fromInt v ++ ".")

                Nothing ->
                    Nothing
        , addDraft = ""
        , removeDraft = ""
    }


{-| Settle a failed edit. Only an in-flight revert resets; an armed
confirmation survives a failed save around it.
-}
failed : Http.Error -> Model -> Model
failed error model =
    let
        wasRevert =
            case model.revert of
                Reverting _ ->
                    True

                _ ->
                    False

        message =
            errorToString error
    in
    { model
        | saving = False
        , revert =
            case model.revert of
                Reverting _ ->
                    Idle

                other ->
                    other
        , status =
            if wasRevert then
                Nothing

            else
                Just message
        , revertStatus =
            if wasRevert then
                Just message

            else
                Nothing
    }


saveTags : Context -> Model -> ( Model, Cmd Msg, List OutMsg )
saveTags ctx model =
    let
        targets =
            targetsFor ctx model

        add =
            splitTagDraft model.addDraft

        remove =
            splitTagDraft model.removeDraft

        id =
            model.requestId + 1
    in
    if not ctx.writesEnabled then
        ( { model | status = Just "Unlock to edit tags." }, Cmd.none, [] )

    else if List.isEmpty targets then
        ( { model | status = Just "Select a post before editing tags." }, Cmd.none, [] )

    else if List.isEmpty add && List.isEmpty remove then
        ( { model | status = Just "Enter a tag to add or remove." }, Cmd.none, [] )

    else
        ( { model | saving = True, status = Nothing, revertStatus = Nothing, requestId = id }
        , Api.Post.editTags ctx.apiBase ctx.credential targets add remove (Completed ctx.accessGeneration id (List.map .id targets))
        , []
        )


requestRevert : Context -> Int -> Model -> ( Model, Cmd Msg, List OutMsg )
requestRevert ctx targetVersion model =
    if not ctx.writesEnabled then
        ( { model | revertStatus = Just "Unlock to revert tags." }, Cmd.none, [] )

    else
        case ctx.activeId of
            Nothing ->
                ( { model | status = Just "Select a post before reverting tags." }, Cmd.none, [] )

            Just _ ->
                case ctx.activeDetail of
                    Just detail ->
                        case List.filter (\r -> r.version == targetVersion) detail.history |> List.head of
                            Just revision ->
                                if not revision.revertible then
                                    ( { model | revertStatus = Just "Legacy revision cannot be reverted." }, Cmd.none, [] )

                                else
                                    case model.revert of
                                        Reverting _ ->
                                            ( model, Cmd.none, [] )

                                        _ ->
                                            ( { model | revert = Confirming targetVersion, revertStatus = Nothing }, Cmd.none, [] )

                            Nothing ->
                                ( { model | revertStatus = Just "Revision not found." }, Cmd.none, [] )

                    Nothing ->
                        ( { model | revert = Confirming targetVersion, revertStatus = Nothing }, Cmd.none, [] )


confirmRevert : Context -> Int -> Model -> ( Model, Cmd Msg, List OutMsg )
confirmRevert ctx targetVersion model =
    if not ctx.writesEnabled then
        ( { model | revertStatus = Just "Unlock to revert tags." }, Cmd.none, [] )

    else
        case ctx.activeId of
            Nothing ->
                ( { model | status = Just "Select a post before reverting tags." }, Cmd.none, [] )

            Just postId ->
                let
                    id =
                        model.requestId + 1

                    target =
                        resolveTarget model ctx postId
                in
                ( { model | saving = True, revert = Reverting targetVersion, revertStatus = Nothing, status = Nothing, requestId = id }
                , Api.Post.revertTags ctx.apiBase ctx.credential [ target ] targetVersion (Completed ctx.accessGeneration id [ postId ])
                , []
                )


splitTagDraft : String -> List String
splitTagDraft draft =
    draft
        |> String.split ","
        |> List.map String.trim
        |> List.filter (String.isEmpty >> not)


errorToString : Http.Error -> String
errorToString error =
    case ApiError.classify error of
        ApiError.BadStatus 409 ->
            "Tags changed elsewhere. Reload the post and try again."

        ApiError.BadStatus 404 ->
            "One selected post no longer exists."

        ApiError.BadStatus code ->
            "Tag edit failed (HTTP " ++ String.fromInt code ++ ")."

        ApiError.BadUrl url ->
            "Invalid API URL: " ++ url

        ApiError.Timeout ->
            "The tag edit timed out."

        ApiError.NetworkError ->
            "The tag edit server could not be reached."

        ApiError.BadBody ->
            "The tag edit response was invalid."
