module Feature.Reactions exposing
    ( Context
    , Model
    , Msg(..)
    , OutMsg(..)
    , activeChanged
    , context
    , draft
    , errorToString
    , init
    , locked
    , noteVersion
    , requestId
    , saving
    , status
    , syncDraft
    , targetsFor
    , update
    , versions
    )

{-| Favorite and score editing: the score draft, saving state, request
generation, known reaction versions, target resolution, and API
commands.

The feature owns the whole request lifecycle. The favorite value derives
from the page's active detail and applies to the selection targets; both
arrive per call through `Context`. Result integration stays with the
caller: `update` reports completions as `OutMsg` values and the page
reloads the active detail. Selection, sequence, detail cache, access
state, and navigation stay with the page.
-}

import Api.Error as ApiError
import Api.Post
import Dict exposing (Dict)
import Domain.Post exposing (PostDetail, ReactionResponse, ReactionTarget)
import Http


type alias Model =
    { scoreDraft : String
    , saving : Bool
    , status : Maybe String
    , requestId : Int
    , versions : Dict String Int
    }


{-| Per-call page facts. Reaction reads are public but edits are
credentialed; `selectedIds` are the current edit targets,
`toggledFavorite` is the negated favorite of the active detail (Nothing
until its details load), and `detailVersion` resolves versions for posts
whose edits never reported one (the feature falls back to it before 0).
-}
type alias Context =
    { apiBase : String
    , credential : Maybe String
    , writesEnabled : Bool
    , accessGeneration : Int
    , selectedIds : List String
    , activeId : Maybe String
    , toggledFavorite : Maybe Bool
    , detailVersion : String -> Maybe Int
    }


context : String -> Maybe String -> Bool -> Int -> List String -> Maybe String -> Maybe Bool -> (String -> Maybe Int) -> Context
context apiBase credential writesEnabled accessGeneration selectedIds activeId toggledFavorite detailVersion =
    { apiBase = apiBase
    , credential = credential
    , writesEnabled = writesEnabled
    , accessGeneration = accessGeneration
    , selectedIds = selectedIds
    , activeId = activeId
    , toggledFavorite = toggledFavorite
    , detailVersion = detailVersion
    }


type Msg
    = DraftChanged String
    | StepScore Int
    | FavoriteRequested
    | ScoreRequested
    | Completed Int Int (List String) (Result Http.Error ReactionResponse)


{-| `Saved` carries the active post for the detail reload; versions
already merged into the feature. `StaleResponse` carries the request's
post ids so the page can reconcile them after the access epoch moved on.
-}
type OutMsg
    = Saved (Maybe String)
    | StaleResponse (List String)
    | AccessRevoked


init : Model
init =
    { scoreDraft = "0"
    , saving = False
    , status = Nothing
    , requestId = 0
    , versions = Dict.empty
    }


draft : Model -> String
draft model =
    model.scoreDraft


saving : Model -> Bool
saving model =
    model.saving


status : Model -> Maybe String
status model =
    model.status


requestId : Model -> Int
requestId model =
    model.requestId


versions : Model -> Dict String Int
versions model =
    model.versions


{-| Record a reaction version sighting (for example from a loaded
detail) so later edits carry version checks.
-}
noteVersion : String -> Int -> Model -> Model
noteVersion postId version model =
    { model | versions = Dict.insert postId version model.versions }


{-| The active detail (re)loaded: the draft follows its score, or resets
to zero without one. The saving flag and versions are untouched.
-}
syncDraft : Maybe PostDetail -> Model -> Model
syncDraft detail model =
    { model
        | scoreDraft =
            detail
                |> Maybe.map (.score >> String.fromInt)
                |> Maybe.withDefault "0"
    }


{-| The active post changed: the notice clears and the draft follows the
new detail (or resets to zero). In-flight work survives untouched.
-}
activeChanged : Maybe PostDetail -> Model -> Model
activeChanged detail model =
    { model | status = Nothing }
        |> syncDraft detail


{-| Credentials went away: the in-flight flag clears because its
completion will arrive with an obsolete generation and be ignored. Draft,
notice, and versions stay.
-}
locked : Model -> Model
locked model =
    { model | saving = False }


resolveTarget : Model -> Context -> String -> ReactionTarget
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
targetsFor : Context -> Model -> List ReactionTarget
targetsFor ctx model =
    List.map (resolveTarget model ctx) ctx.selectedIds


update : Context -> Msg -> Model -> ( Model, Cmd Msg, List OutMsg )
update ctx msg model =
    case msg of
        DraftChanged value ->
            ( { model | scoreDraft = value, status = Nothing }, Cmd.none, [] )

        StepScore delta ->
            -- Keyboard stepping adjusts the draft and saves it, clamped
            -- at zero; an unparseable draft counts as zero.
            update ctx ScoreRequested
                { model
                    | scoreDraft = String.fromInt (max 0 ((String.toInt model.scoreDraft |> Maybe.withDefault 0) + delta))
                    , status = Nothing
                }

        FavoriteRequested ->
            saveFavorite ctx model

        ScoreRequested ->
            saveScore ctx model

        Completed generation id postIds result ->
            if generation /= ctx.accessGeneration then
                ( model, Cmd.none, [ StaleResponse postIds ] )

            else if id /= model.requestId then
                ( model, Cmd.none, [] )

            else
                case result of
                    Ok response ->
                        let
                            merged =
                                List.foldl (\post versions_ -> Dict.insert post.id post.version versions_) model.versions response.posts
                        in
                        ( { model | versions = merged, saving = False, status = Just "Favorite or score saved." }
                        , Cmd.none
                        , [ Saved ctx.activeId ]
                        )

                    Err (Http.BadStatus 401) ->
                        ( model, Cmd.none, [ AccessRevoked ] )

                    Err error ->
                        ( { model | saving = False, status = Just (errorToString error) }, Cmd.none, [] )


saveFavorite : Context -> Model -> ( Model, Cmd Msg, List OutMsg )
saveFavorite ctx model =
    let
        targets =
            targetsFor ctx model

        id =
            model.requestId + 1
    in
    if not ctx.writesEnabled then
        ( { model | status = Just "Unlock to change favorites." }, Cmd.none, [] )

    else
        case ctx.toggledFavorite of
            Nothing ->
                ( { model | status = Just "Select a post and load its details before changing favorite." }, Cmd.none, [] )

            Just value ->
                if List.isEmpty targets then
                    ( { model | status = Just "Select a post before changing favorite." }, Cmd.none, [] )

                else
                    ( { model | saving = True, status = Nothing, requestId = id }
                    , Api.Post.editReactions ctx.apiBase ctx.credential targets (Just value) Nothing (Completed ctx.accessGeneration id (List.map .id targets))
                    , []
                    )


saveScore : Context -> Model -> ( Model, Cmd Msg, List OutMsg )
saveScore ctx model =
    let
        targets =
            targetsFor ctx model

        id =
            model.requestId + 1
    in
    if not ctx.writesEnabled then
        ( { model | status = Just "Unlock to change the score." }, Cmd.none, [] )

    else
        case String.toInt (String.trim model.scoreDraft) of
            Nothing ->
                ( { model | status = Just "Score must be a non-negative integer." }, Cmd.none, [] )

            Just score ->
                if score < 0 then
                    ( { model | status = Just "Score must be a non-negative integer." }, Cmd.none, [] )

                else if List.isEmpty targets then
                    ( { model | status = Just "Select a post before changing score." }, Cmd.none, [] )

                else
                    ( { model | saving = True, status = Nothing, requestId = id }
                    , Api.Post.editReactions ctx.apiBase ctx.credential targets Nothing (Just score) (Completed ctx.accessGeneration id (List.map .id targets))
                    , []
                    )


errorToString : Http.Error -> String
errorToString error =
    case ApiError.classify error of
        ApiError.BadStatus 409 ->
            "Rating changed elsewhere. Reload the post and try again."

        ApiError.BadStatus 404 ->
            "One selected post no longer exists."

        ApiError.BadStatus code ->
            "Rating edit failed (HTTP " ++ String.fromInt code ++ ")."

        ApiError.BadUrl url ->
            "Invalid API URL: " ++ url

        ApiError.Timeout ->
            "The rating edit timed out."

        ApiError.NetworkError ->
            "The rating edit server could not be reached."

        ApiError.BadBody ->
            "The rating response was invalid."
